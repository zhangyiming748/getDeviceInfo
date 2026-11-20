package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procSetConsoleOutputCP = kernel32.NewProc("SetConsoleOutputCP")
	procSetConsoleCP       = kernel32.NewProc("SetConsoleCP")
	user32                 = syscall.NewLazyDLL("user32.dll")
	procMessageBoxW        = user32.NewProc("MessageBoxW")
)

const (
	mbYesNo           = 0x00000004
	mbIconError       = 0x00000010
	mbIconQuestion    = 0x00000020
	mbIconInformation = 0x00000040
	mbSetForeground   = 0x00010000
	idYes             = 6
)

func init() {
	// 控制台构建时让中文提示正常显示；GUI 构建时该调用无副作用
	procSetConsoleOutputCP.Call(65001)
	procSetConsoleCP.Call(65001)
}

func main() {
	out := flag.String("out", "", "输出 CSV 文件路径，默认为当前用户桌面")
	noUI := flag.Bool("no-ui", false, "不弹出提示框，仅输出到控制台（自动化/无人值守用）")
	flag.Parse()

	info, warnings := collectDeviceInfo()
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "警告: "+w)
	}

	target := strings.TrimSpace(*out)
	if target == "" {
		dir, err := desktopDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "警告: "+err.Error()+"，改为保存到当前目录")
			dir = "."
		}
		fileName := fmt.Sprintf("设备台账_%s_%s.csv",
			sanitizeFileName(info.ComputerName), time.Now().Format("20060102_150405"))
		target = filepath.Join(dir, fileName)
	} else if !strings.EqualFold(filepath.Ext(target), ".csv") {
		target += ".csv"
	}

	abs, err := filepath.Abs(target)
	if err != nil {
		abs = target
	}
	if err := writeCSV(abs, info); err != nil {
		fmt.Fprintln(os.Stderr, "生成 CSV 失败: "+err.Error())
		if !*noUI {
			messageBox("设备信息采集失败：\n\n"+err.Error()+"\n\n请联系管理员处理。",
				"设备信息采集失败", mbIconError|mbSetForeground)
		}
		os.Exit(1)
	}

	fmt.Println("已生成设备台账: " + abs)
	if *noUI {
		return
	}
	if messageBox("设备信息采集完成。\n\n已生成台账文件：\n"+abs+"\n\n是否打开文件所在位置？",
		"设备信息采集完成", mbYesNo|mbIconInformation|mbSetForeground) == idYes {
		openFileLocation(abs)
	}
}

// messageBox 弹出系统消息框；没有桌面会话（如服务方式运行）时返回 0
func messageBox(text, caption string, flags uintptr) int {
	t, err := syscall.UTF16PtrFromString(text)
	if err != nil {
		return 0
	}
	c, err := syscall.UTF16PtrFromString(caption)
	if err != nil {
		return 0
	}
	ret, _, _ := procMessageBoxW.Call(0,
		uintptr(unsafe.Pointer(t)),
		uintptr(unsafe.Pointer(c)),
		flags,
	)
	return int(ret)
}

func openFileLocation(path string) {
	_ = exec.Command("explorer.exe", "/select,"+path).Start()
}

// desktopDir 通过注册表获取桌面目录，可正确处理 OneDrive 重定向等情况
func desktopDir() (string, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Explorer\User Shell Folders`, registry.QUERY_VALUE)
	if err == nil {
		defer key.Close()
		if v, _, err := key.GetStringValue("Desktop"); err == nil && v != "" {
			v = os.ExpandEnv(v)
			if st, err := os.Stat(v); err == nil && st.IsDir() {
				return v, nil
			}
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	desktop := filepath.Join(home, "Desktop")
	if st, err := os.Stat(desktop); err == nil && st.IsDir() {
		return desktop, nil
	}
	return home, nil
}

func sanitizeFileName(s string) string {
	if s == "" {
		return "unknown"
	}
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\/:*?"<>|`, r) {
			return '_'
		}
		return r
	}, s)
}
