package main

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"syscall"
	"unsafe"

	"github.com/yusufpapurcu/wmi"
)

type win32Printer struct {
	Name     string
	PortName string
	Default  bool
}

type win32TCPIPPrinterPort struct {
	Name        string
	HostAddress string
}

type PrinterInfo struct {
	Name     string
	IP       string
	MAC      string
	PortName string
}

func collectPrinters() ([]PrinterInfo, []string) {
	var printers []win32Printer
	if err := wmi.Query("SELECT Name, PortName, Default FROM Win32_Printer", &printers); err != nil {
		return nil, []string{"读取打印机列表失败: " + err.Error()}
	}

	// 标准 TCP/IP 打印机端口中保存着目标地址
	hostByPort := map[string]string{}
	var ports []win32TCPIPPrinterPort
	if err := wmi.Query("SELECT Name, HostAddress FROM Win32_TCPIPPrinterPort", &ports); err == nil {
		for _, p := range ports {
			if p.HostAddress != "" {
				hostByPort[p.Name] = p.HostAddress
			}
		}
	}

	// 默认打印机排在最前，其余按名称排序
	sort.SliceStable(printers, func(i, j int) bool {
		if printers[i].Default != printers[j].Default {
			return printers[i].Default
		}
		return printers[i].Name < printers[j].Name
	})

	result := make([]PrinterInfo, 0, len(printers))
	for _, p := range printers {
		info := PrinterInfo{
			Name:     strings.TrimSpace(p.Name),
			PortName: strings.TrimSpace(p.PortName),
		}

		if host, ok := hostByPort[info.PortName]; ok {
			info.IP = resolveIP(host)
		}
		if info.IP == "" {
			info.IP = ipFromPortName(info.PortName)
		}
		if info.IP != "" {
			info.MAC = arpLookup(info.IP)
		}

		result = append(result, info)
	}
	return result, nil
}

// portLabel 将端口名转换为便于阅读的类型
func portLabel(port string) string {
	upper := strings.ToUpper(port)
	switch {
	case strings.HasPrefix(upper, "WSD-"):
		return "WSD 端口"
	case strings.HasPrefix(port, `\\`) || strings.HasPrefix(port, `//`):
		return "共享打印机端口"
	case strings.HasPrefix(upper, "IP_"):
		return "TCP/IP 端口"
	case strings.HasPrefix(upper, "USB"):
		return "USB 端口"
	}
	return port
}

// ipFromPortName 解析 "IP_192.168.1.20" 形式端口，以及 "\\server\printer" 共享端口
func ipFromPortName(port string) string {
	upper := strings.ToUpper(port)
	if strings.HasPrefix(upper, "IP_") {
		return resolveIP(strings.TrimSpace(port[3:]))
	}
	if strings.HasPrefix(port, `\\`) || strings.HasPrefix(port, `//`) {
		rest := strings.TrimPrefix(strings.TrimPrefix(port, `\\`), `//`)
		host := rest
		if i := strings.IndexAny(rest, `\/`); i > 0 {
			host = rest[:i]
		}
		return resolveIP(host)
	}
	return ""
}

func resolveIP(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	addrs, err := net.LookupHost(host)
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if ip := net.ParseIP(a); ip != nil && ip.To4() != nil {
			return ip.String()
		}
	}
	if len(addrs) > 0 {
		return addrs[0]
	}
	return ""
}

var procSendARP = syscall.NewLazyDLL("iphlpapi.dll").NewProc("SendARP")

// arpLookup 通过 ARP 查询同网段设备的 MAC 地址，跨网段或设备不可达时返回空
func arpLookup(ipStr string) string {
	ip := net.ParseIP(ipStr).To4()
	if ip == nil {
		return ""
	}
	dst := uint32(ip[0]) | uint32(ip[1])<<8 | uint32(ip[2])<<16 | uint32(ip[3])<<24

	var mac [8]byte
	size := uint32(len(mac))
	ret, _, _ := procSendARP.Call(
		uintptr(dst),
		0,
		uintptr(unsafe.Pointer(&mac[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if ret != 0 || size == 0 || size > uint32(len(mac)) {
		return ""
	}

	parts := make([]string, 0, size)
	for i := uint32(0); i < size; i++ {
		parts = append(parts, fmt.Sprintf("%02X", mac[i]))
	}
	return strings.Join(parts, ":")
}
