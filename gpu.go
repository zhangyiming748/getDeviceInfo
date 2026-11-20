package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/yusufpapurcu/wmi"
	"golang.org/x/sys/windows/registry"
)

// 显示适配器驱动类注册表路径，用于读取准确的显存大小（WMI 的 AdapterRAM 会溢出）
const displayClassKey = `SYSTEM\CurrentControlSet\Control\Class\{4d36e968-e325-11ce-bfc1-08002be10318}`

type win32VideoController struct {
	Name       string
	AdapterRAM uint32
}

type GPUInfo struct {
	Name        string
	VRAMMB      uint64
	ClockMHz    uint32
	MemType     string
	MemClockMHz uint32
}

func collectGPUs() ([]GPUInfo, []string) {
	var controllers []win32VideoController
	if err := wmi.Query("SELECT Name, AdapterRAM FROM Win32_VideoController", &controllers); err != nil {
		return nil, []string{"读取显卡信息失败: " + err.Error()}
	}

	vram := gpuVRAMFromRegistry()
	smi := nvidiaSMIInfo()

	var gpus []GPUInfo
	for _, c := range controllers {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}

		g := GPUInfo{Name: name}

		bytes, ok := lookupVRAM(vram, name)
		integrated := false
		if ok && bytes > 0 {
			g.VRAMMB = bytes / (1024 * 1024)
		} else if integratedGPU(name) {
			integrated = true
		} else if c.AdapterRAM > 0 {
			g.VRAMMB = uint64(c.AdapterRAM) / (1024 * 1024)
		}

		if s, ok := lookupSMI(smi, name); ok {
			g.ClockMHz = s.graphicsClock
			g.MemClockMHz = s.memClock
			if g.VRAMMB == 0 && s.memTotalMB > 0 {
				g.VRAMMB = s.memTotalMB
			}
		}

		switch {
		case integrated:
			g.MemType = "共享内存"
		case g.VRAMMB > 0:
			g.MemType = "GDDR"
		}

		gpus = append(gpus, g)
	}
	return gpus, nil
}

// gpuVRAMFromRegistry 读取每个显卡驱动的 HardwareInformation.qwMemorySize（单位：字节）
func gpuVRAMFromRegistry() map[string]uint64 {
	result := map[string]uint64{}

	root, err := registry.OpenKey(registry.LOCAL_MACHINE, displayClassKey, registry.READ)
	if err != nil {
		return result
	}
	defer root.Close()

	subKeys, err := root.ReadSubKeyNames(-1)
	if err != nil {
		return result
	}
	for _, sub := range subKeys {
		if len(sub) != 4 {
			continue
		}
		if _, err := strconv.Atoi(sub); err != nil {
			continue
		}

		key, err := registry.OpenKey(registry.LOCAL_MACHINE, displayClassKey+`\`+sub, registry.READ)
		if err != nil {
			continue
		}
		desc, _, descErr := key.GetStringValue("DriverDesc")
		if descErr == nil && desc != "" {
			if v, _, err := key.GetIntegerValue("HardwareInformation.qwMemorySize"); err == nil && v > 0 {
				result[strings.ToLower(strings.TrimSpace(desc))] = v
			}
		}
		key.Close()
	}
	return result
}

func lookupVRAM(m map[string]uint64, name string) (uint64, bool) {
	l := strings.ToLower(name)
	if v, ok := m[l]; ok {
		return v, true
	}
	for k, v := range m {
		if strings.Contains(k, l) || strings.Contains(l, k) {
			return v, true
		}
	}
	return 0, false
}

func integratedGPU(name string) bool {
	l := strings.ToLower(name)
	if strings.Contains(l, "intel") {
		return true
	}
	if strings.Contains(l, "radeon") && !strings.Contains(l, "rx ") && !strings.Contains(l, "pro ") {
		return true
	}
	return false
}

// ---------- nvidia-smi：补充显卡核心/显存频率 ----------

type smiInfo struct {
	memTotalMB    uint64
	graphicsClock uint32
	memClock      uint32
}

func nvidiaSMIInfo() map[string]smiInfo {
	candidates := []string{"nvidia-smi.exe"}
	if sysRoot := os.Getenv("SystemRoot"); sysRoot != "" {
		candidates = append(candidates, filepath.Join(sysRoot, "System32", "nvidia-smi.exe"))
	}
	candidates = append(candidates, `C:\Program Files\NVIDIA Corporation\NVSMI\nvidia-smi.exe`)

	args := []string{
		"--query-gpu=name,memory.total,clocks.max.graphics,clocks.max.memory",
		"--format=csv,noheader,nounits",
	}
	for _, exe := range candidates {
		if strings.ContainsAny(exe, `\/:`) {
			if _, err := os.Stat(exe); err != nil {
				continue
			}
		}
		out, err := runHidden(exe, args...)
		if err != nil {
			continue
		}
		if result := parseSMIOutput(string(out)); len(result) > 0 {
			return result
		}
	}
	return nil
}

func parseSMIOutput(out string) map[string]smiInfo {
	result := map[string]smiInfo{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) < 4 {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(fields[0]))
		info := smiInfo{
			memTotalMB:    parseUintField(fields[1]),
			graphicsClock: uint32(parseUintField(fields[2])),
			memClock:      uint32(parseUintField(fields[3])),
		}
		result[name] = info
	}
	return result
}

func parseUintField(s string) uint64 {
	v, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func lookupSMI(m map[string]smiInfo, name string) (smiInfo, bool) {
	l := strings.ToLower(name)
	if v, ok := m[l]; ok {
		return v, true
	}
	for k, v := range m {
		if strings.Contains(k, l) || strings.Contains(l, k) {
			return v, true
		}
	}
	return smiInfo{}, false
}

func runHidden(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Output()
}
