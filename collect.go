package main

import (
	"encoding/csv"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/yusufpapurcu/wmi"
)

// ---------- WMI 映射结构 ----------

type win32ComputerSystem struct {
	Name string
}

type win32Processor struct {
	Name                      string
	NumberOfCores             uint32
	NumberOfLogicalProcessors uint32
	MaxClockSpeed             uint32
}

type win32PhysicalMemory struct {
	Capacity             uint64
	SMBIOSMemoryType     uint32
	Speed                uint32
	ConfiguredClockSpeed uint32
}

type win32NetworkAdapterConfiguration struct {
	Description string
	MACAddress  string
	IPAddress   []string
	IPEnabled   bool
	DNSHostName string
}

type win32NetworkAdapter struct {
	Name            string
	NetConnectionID string
	MACAddress      string
}

// ---------- 汇总后的设备信息 ----------

type DeviceInfo struct {
	ComputerName string
	Hostname     string

	IPs  []string
	MACs []string

	CPUModel    string
	CPUCores    uint32
	CPUThreads  uint32
	CPUClockMHz uint32

	MemTotalGB string
	MemTypes   []string
	MemFreqs   []string

	GPUs     []GPUInfo
	Printers []PrinterInfo
}

func collectDeviceInfo() (*DeviceInfo, []string) {
	info := &DeviceInfo{}
	var warnings []string

	var systems []win32ComputerSystem
	if err := wmi.Query("SELECT Name FROM Win32_ComputerSystem", &systems); err != nil {
		warnings = append(warnings, "读取电脑名称失败: "+err.Error())
	} else if len(systems) > 0 {
		info.ComputerName = strings.TrimSpace(systems[0].Name)
	}

	info.Hostname, _ = os.Hostname()
	if info.ComputerName == "" {
		info.ComputerName = info.Hostname
	}

	collectCPU(info, &warnings)
	collectMemory(info, &warnings)
	collectNetwork(info, &warnings)

	gpus, gpuWarnings := collectGPUs()
	info.GPUs = gpus
	warnings = append(warnings, gpuWarnings...)

	printers, printerWarnings := collectPrinters()
	info.Printers = printers
	warnings = append(warnings, printerWarnings...)

	return info, warnings
}

func collectCPU(info *DeviceInfo, warnings *[]string) {
	var cpus []win32Processor
	if err := wmi.Query("SELECT Name, NumberOfCores, NumberOfLogicalProcessors, MaxClockSpeed FROM Win32_Processor", &cpus); err != nil {
		*warnings = append(*warnings, "读取 CPU 信息失败: "+err.Error())
		return
	}

	var names []string
	seen := map[string]bool{}
	for _, c := range cpus {
		name := strings.TrimSpace(c.Name)
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
		info.CPUCores += c.NumberOfCores
		info.CPUThreads += c.NumberOfLogicalProcessors
		if c.MaxClockSpeed > info.CPUClockMHz {
			info.CPUClockMHz = c.MaxClockSpeed
		}
	}
	info.CPUModel = strings.Join(names, "/")
}

func collectMemory(info *DeviceInfo, warnings *[]string) {
	var modules []win32PhysicalMemory
	if err := wmi.Query("SELECT Capacity, SMBIOSMemoryType, Speed, ConfiguredClockSpeed FROM Win32_PhysicalMemory", &modules); err != nil {
		*warnings = append(*warnings, "读取内存信息失败: "+err.Error())
		return
	}

	var total uint64
	seenType := map[string]bool{}
	seenFreq := map[string]bool{}
	for _, m := range modules {
		if m.Capacity == 0 {
			continue
		}
		total += m.Capacity

		if t := memoryTypeName(m.SMBIOSMemoryType); t != "" && !seenType[t] {
			seenType[t] = true
			info.MemTypes = append(info.MemTypes, t)
		}

		freq := m.ConfiguredClockSpeed
		if freq == 0 {
			freq = m.Speed
		}
		if freq > 0 {
			f := strconv.FormatUint(uint64(freq), 10)
			if !seenFreq[f] {
				seenFreq[f] = true
				info.MemFreqs = append(info.MemFreqs, f)
			}
		}
	}

	gb := float64(total) / (1024 * 1024 * 1024)
	if gb == math.Trunc(gb) {
		info.MemTotalGB = strconv.FormatFloat(gb, 'f', 0, 64)
	} else {
		info.MemTotalGB = strconv.FormatFloat(gb, 'f', 1, 64)
	}
}

func collectNetwork(info *DeviceInfo, warnings *[]string) {
	var configs []win32NetworkAdapterConfiguration
	if err := wmi.Query("SELECT Description, MACAddress, IPAddress, IPEnabled, DNSHostName FROM Win32_NetworkAdapterConfiguration WHERE IPEnabled = TRUE", &configs); err != nil {
		*warnings = append(*warnings, "读取网卡 IP 信息失败: "+err.Error())
	} else {
		var v4, v6 []string
		seenIP := map[string]bool{}
		for _, c := range configs {
			if info.Hostname == "" && c.DNSHostName != "" {
				info.Hostname = c.DNSHostName
			}
			for _, raw := range c.IPAddress {
				ip := strings.TrimSpace(raw)
				if !usableIP(ip) || seenIP[ip] {
					continue
				}
				seenIP[ip] = true
				if strings.Contains(ip, ":") {
					v6 = append(v6, ip)
				} else {
					v4 = append(v4, ip)
				}
			}
		}
		info.IPs = append(v4, v6...)
	}

	var adapters []win32NetworkAdapter
	if err := wmi.Query("SELECT Name, NetConnectionID, MACAddress FROM Win32_NetworkAdapter", &adapters); err != nil {
		*warnings = append(*warnings, "读取网卡 MAC 信息失败: "+err.Error())
		return
	}
	seenMAC := map[string]bool{}
	for _, a := range adapters {
		mac := strings.ToUpper(strings.TrimSpace(a.MACAddress))
		if mac == "" || seenMAC[mac] || ignoredAdapter(a.Name) {
			continue
		}
		seenMAC[mac] = true
		info.MACs = append(info.MACs, mac)
	}
}

func usableIP(s string) bool {
	ip := net.ParseIP(s)
	if ip == nil {
		return false
	}
	return !(ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast())
}

func ignoredAdapter(name string) bool {
	l := strings.ToLower(name)
	for _, kw := range []string{"wan miniport", "loopback", "kernel debug", "tunnel", "bluetooth"} {
		if strings.Contains(l, kw) {
			return true
		}
	}
	return false
}

func memoryTypeName(code uint32) string {
	switch code {
	case 20:
		return "DDR"
	case 21:
		return "DDR2"
	case 22:
		return "DDR2 FB-DIMM"
	case 24:
		return "DDR3"
	case 26:
		return "DDR4"
	case 27:
		return "LPDDR"
	case 28:
		return "LPDDR2"
	case 29:
		return "LPDDR3"
	case 30:
		return "LPDDR4"
	case 32:
		return "HBM"
	case 33:
		return "HBM2"
	case 34:
		return "DDR5"
	case 35:
		return "LPDDR5"
	case 36:
		return "HBM3"
	default:
		return ""
	}
}

// ---------- CSV 输出 ----------

var csvHeader = []string{
	"电脑名称", "Hostname", "IP地址", "MAC地址",
	"CPU型号", "CPU核心数", "CPU线程数", "CPU频率(MHz)",
	"内存大小(GB)", "内存类型", "内存频率(MHz)",
	"显卡型号", "显卡显存(MB)", "显卡频率(MHz)", "显卡内存类型", "显卡内存频率(MHz)",
	"打印机名称", "打印机IP地址", "打印机MAC地址", "打印机端口",
}

func writeCSV(path string, info *DeviceInfo) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Windows 版 Excel 依赖 BOM 才能正确识别 UTF-8 中文
	if _, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return err
	}

	w := csv.NewWriter(f)
	w.UseCRLF = true
	if err := w.Write(csvHeader); err != nil {
		return err
	}
	if err := w.Write(buildRecord(info)); err != nil {
		return err
	}
	w.Flush()
	return w.Error()
}

func buildRecord(info *DeviceInfo) []string {
	return []string{
		info.ComputerName,
		info.Hostname,
		strings.Join(info.IPs, "/"),
		strings.Join(info.MACs, "/"),
		info.CPUModel,
		formatUint(uint64(info.CPUCores)),
		formatUint(uint64(info.CPUThreads)),
		formatUint(uint64(info.CPUClockMHz)),
		info.MemTotalGB,
		strings.Join(info.MemTypes, "/"),
		strings.Join(info.MemFreqs, "/"),
		gpuCells(info.GPUs, func(g GPUInfo) string { return g.Name }),
		gpuCells(info.GPUs, func(g GPUInfo) string { return formatUint(g.VRAMMB) }),
		gpuCells(info.GPUs, func(g GPUInfo) string { return formatUint(uint64(g.ClockMHz)) }),
		gpuCells(info.GPUs, func(g GPUInfo) string { return g.MemType }),
		gpuCells(info.GPUs, func(g GPUInfo) string { return formatUint(uint64(g.MemClockMHz)) }),
		printerCells(info.Printers, func(p PrinterInfo) string { return p.Name }),
		printerCells(info.Printers, func(p PrinterInfo) string { return p.IP }),
		printerCells(info.Printers, func(p PrinterInfo) string { return p.MAC }),
		printerCells(info.Printers, func(p PrinterInfo) string { return portLabel(p.PortName) }),
	}
}

// gpuCells 按显卡顺序拼接，缺失值用 "-" 占位以保证各列对齐
func gpuCells(gpus []GPUInfo, get func(GPUInfo) string) string {
	if len(gpus) == 0 {
		return ""
	}
	parts := make([]string, 0, len(gpus))
	for _, g := range gpus {
		v := get(g)
		if v == "" {
			v = "-"
		}
		parts = append(parts, v)
	}
	return strings.Join(parts, "/")
}

// printerCells 只拼接非空值，避免大量占位符
func printerCells(printers []PrinterInfo, get func(PrinterInfo) string) string {
	var parts []string
	for _, p := range printers {
		if v := get(p); v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, "/")
}

func formatUint(v uint64) string {
	if v == 0 {
		return ""
	}
	return strconv.FormatUint(v, 10)
}
