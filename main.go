package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

var (
	kernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procSetConsoleOutputCP    = kernel32.NewProc("SetConsoleOutputCP")
	procSetConsoleCP          = kernel32.NewProc("SetConsoleCP")
	procGetConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
)

func init() {
	// 让控制台以 UTF-8 显示中文，避免提示信息乱码
	procSetConsoleOutputCP.Call(65001)
	procSetConsoleCP.Call(65001)
}

func main() {
	out := flag.String("out", "", "输出 CSV 文件路径，默认为当前用户桌面")
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
		pauseIfNeeded()
		os.Exit(1)
	}

	fmt.Println("已生成设备台账: " + abs)
	pauseIfNeeded()
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

// pauseIfNeeded 在双击运行时（控制台里只有当前进程）等待回车，避免窗口一闪而过
func pauseIfNeeded() {
	var pids [4]uint32
	n, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	if n > 1 {
		return
	}
	fmt.Print("按回车键退出...")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}
