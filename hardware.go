package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/yusufpapurcu/wmi"
)

// 新增硬件字段可能由 OEM 留空，使用独立客户端兼容空值，不影响原有采集。
var hardwareWMI = &wmi.Client{NonePtrZero: true, PtrNil: true}

type win32ComputerSystemProduct struct {
	IdentifyingNumber string
	UUID              string
}

type win32BIOS struct {
	Manufacturer      string
	SMBIOSBIOSVersion string
	Version           string
	ReleaseDate       string
	SerialNumber      string
}

type win32SystemEnclosure struct {
	SerialNumber   string
	SMBIOSAssetTag string
	ChassisTypes   []int32
}

type win32BaseBoard struct {
	Manufacturer string
	Product      string
	Version      string
	SerialNumber string
}

type win32DiskDrive struct {
	DeviceID      string
	Model         string
	SerialNumber  string
	Size          *uint64
	InterfaceType string
	MediaType     string
}

type win32LogicalDisk struct {
	DeviceID   string
	VolumeName string
	FileSystem string
	Size       *uint64
	FreeSpace  *uint64
}

type wmiMonitorID struct {
	InstanceName     string
	ManufacturerName []int32
	UserFriendlyName []int32
	ProductCodeID    []int32
	SerialNumberID   []int32
}

type win32SoundDevice struct {
	Name         string
	Manufacturer string
	PNPDeviceID  string
}

type win32PhysicalAdapter struct {
	DeviceID string
	Name     string
	Speed    *uint64
}

type win32Battery struct {
	DeviceID                 string
	Name                     string
	EstimatedChargeRemaining *uint16
	BatteryStatus            *uint16
	DesignCapacity           *uint32
	FullChargeCapacity       *uint32
}

type MemoryModuleInfo struct {
	Slot         string
	Manufacturer string
	PartNumber   string
}

type DiskInfo struct {
	DeviceID      string
	Model         string
	SerialNumber  string
	SizeGB        string
	InterfaceType string
	MediaType     string
}

type VolumeInfo struct {
	DeviceID   string
	VolumeName string
	FileSystem string
	SizeGB     string
	FreeGB     string
}

type MonitorInfo struct {
	InstanceName string
	Manufacturer string
	Model        string
	SerialNumber string
}

type SoundDeviceInfo struct {
	Name         string
	Manufacturer string
	PNPDeviceID  string
}

type PhysicalNICInfo struct {
	DeviceID  string
	Name      string
	SpeedMbps string
}

type BatteryInfo struct {
	DeviceID              string
	Name                  string
	ChargePercent         string
	Status                string
	DesignCapacityMWh     string
	FullChargeCapacityMWh string
}

type HardwareInfo struct {
	Manufacturer string
	SerialNumber string
	UUID         string
	ChassisType  string
	AssetTag     string

	BaseboardManufacturer string
	BaseboardModel        string
	BaseboardVersion      string
	BaseboardSerialNumber string
	BIOSManufacturer      string
	BIOSVersion           string
	BIOSReleaseDate       string

	CPUSockets    []string
	MemoryModules []MemoryModuleInfo
	Disks         []DiskInfo
	Volumes       []VolumeInfo
	Monitors      []MonitorInfo
	SoundDevices  []SoundDeviceInfo
	PhysicalNICs  []PhysicalNICInfo
	Batteries     []BatteryInfo
}

func collectHardware(info *HardwareInfo, warnings *[]string) {
	collectSystemHardware(info, warnings)
	collectDisks(info, warnings)
	collectVolumes(info, warnings)
	collectMonitors(info, warnings)
	collectSoundDevices(info, warnings)
	collectPhysicalNICs(info, warnings)
	collectBatteries(info, warnings)
	sortHardware(info)
}

func collectSystemHardware(info *HardwareInfo, warnings *[]string) {
	var products []win32ComputerSystemProduct
	if queryHardware("整机资产信息", "SELECT IdentifyingNumber, UUID FROM Win32_ComputerSystemProduct", &products, warnings) && len(products) > 0 {
		info.SerialNumber = strings.TrimSpace(products[0].IdentifyingNumber)
		info.UUID = strings.TrimSpace(products[0].UUID)
	}

	var biosRows []win32BIOS
	if queryHardware("BIOS 信息", "SELECT Manufacturer, SMBIOSBIOSVersion, Version, ReleaseDate, SerialNumber FROM Win32_BIOS", &biosRows, warnings) && len(biosRows) > 0 {
		bios := biosRows[0]
		info.BIOSManufacturer = strings.TrimSpace(bios.Manufacturer)
		info.BIOSVersion = strings.TrimSpace(bios.SMBIOSBIOSVersion)
		if info.BIOSVersion == "" {
			info.BIOSVersion = strings.TrimSpace(bios.Version)
		}
		info.BIOSReleaseDate = formatWMIDate(bios.ReleaseDate)
		if info.SerialNumber == "" {
			info.SerialNumber = strings.TrimSpace(bios.SerialNumber)
		}
	}

	var enclosures []win32SystemEnclosure
	if queryHardware("机箱信息", "SELECT SerialNumber, SMBIOSAssetTag, ChassisTypes FROM Win32_SystemEnclosure", &enclosures, warnings) {
		var chassisTypes []string
		seenType := map[string]bool{}
		for _, enclosure := range enclosures {
			if info.SerialNumber == "" {
				info.SerialNumber = strings.TrimSpace(enclosure.SerialNumber)
			}
			if info.AssetTag == "" {
				info.AssetTag = strings.TrimSpace(enclosure.SMBIOSAssetTag)
			}
			for _, code := range enclosure.ChassisTypes {
				name := chassisTypeName(code)
				if name != "" && !seenType[name] {
					seenType[name] = true
					chassisTypes = append(chassisTypes, name)
				}
			}
		}
		info.ChassisType = strings.Join(chassisTypes, "/")
	}

	var boards []win32BaseBoard
	if queryHardware("主板信息", "SELECT Manufacturer, Product, Version, SerialNumber FROM Win32_BaseBoard", &boards, warnings) && len(boards) > 0 {
		board := boards[0]
		info.BaseboardManufacturer = strings.TrimSpace(board.Manufacturer)
		info.BaseboardModel = strings.TrimSpace(board.Product)
		info.BaseboardVersion = strings.TrimSpace(board.Version)
		info.BaseboardSerialNumber = strings.TrimSpace(board.SerialNumber)
	}
}

func collectDisks(info *HardwareInfo, warnings *[]string) {
	var rows []win32DiskDrive
	if !queryHardware("物理硬盘信息", "SELECT DeviceID, Model, SerialNumber, Size, InterfaceType, MediaType FROM Win32_DiskDrive", &rows, warnings) {
		return
	}
	for _, row := range rows {
		deviceID := strings.TrimSpace(row.DeviceID)
		model := strings.TrimSpace(row.Model)
		if deviceID == "" && model == "" {
			continue
		}
		info.Disks = append(info.Disks, DiskInfo{
			DeviceID:      deviceID,
			Model:         model,
			SerialNumber:  strings.TrimSpace(row.SerialNumber),
			SizeGB:        formatBytesGB(row.Size),
			InterfaceType: strings.TrimSpace(row.InterfaceType),
			MediaType:     strings.TrimSpace(row.MediaType),
		})
	}
}

func collectVolumes(info *HardwareInfo, warnings *[]string) {
	var rows []win32LogicalDisk
	if !queryHardware("逻辑分区信息", "SELECT DeviceID, VolumeName, FileSystem, Size, FreeSpace FROM Win32_LogicalDisk WHERE DriveType = 3", &rows, warnings) {
		return
	}
	for _, row := range rows {
		deviceID := strings.TrimSpace(row.DeviceID)
		if deviceID == "" {
			continue
		}
		info.Volumes = append(info.Volumes, VolumeInfo{
			DeviceID:   deviceID,
			VolumeName: strings.TrimSpace(row.VolumeName),
			FileSystem: strings.TrimSpace(row.FileSystem),
			SizeGB:     formatBytesGB(row.Size),
			FreeGB:     formatBytesGBAllowZero(row.FreeSpace),
		})
	}
}

func collectMonitors(info *HardwareInfo, warnings *[]string) {
	var rows []wmiMonitorID
	if !queryHardwareNamespace("显示器信息", "SELECT InstanceName, ManufacturerName, UserFriendlyName, ProductCodeID, SerialNumberID FROM WmiMonitorID WHERE Active = TRUE", `root\wmi`, &rows, warnings) {
		return
	}
	for _, row := range rows {
		monitor := MonitorInfo{
			InstanceName: strings.TrimSpace(row.InstanceName),
			Manufacturer: decodeWMIString(row.ManufacturerName),
			Model:        decodeWMIString(row.UserFriendlyName),
			SerialNumber: decodeWMIString(row.SerialNumberID),
		}
		if monitor.Model == "" {
			monitor.Model = decodeWMIString(row.ProductCodeID)
		}
		if monitor.Manufacturer == "" && monitor.Model == "" && monitor.SerialNumber == "" {
			continue
		}
		info.Monitors = append(info.Monitors, monitor)
	}
}

func collectSoundDevices(info *HardwareInfo, warnings *[]string) {
	var rows []win32SoundDevice
	if !queryHardware("声卡信息", "SELECT Name, Manufacturer, PNPDeviceID FROM Win32_SoundDevice", &rows, warnings) {
		return
	}
	for _, row := range rows {
		device := SoundDeviceInfo{
			Name:         strings.TrimSpace(row.Name),
			Manufacturer: strings.TrimSpace(row.Manufacturer),
			PNPDeviceID:  strings.TrimSpace(row.PNPDeviceID),
		}
		if device.Name == "" && device.Manufacturer == "" {
			continue
		}
		info.SoundDevices = append(info.SoundDevices, device)
	}
}

func collectPhysicalNICs(info *HardwareInfo, warnings *[]string) {
	var rows []win32PhysicalAdapter
	if !queryHardware("物理网卡信息", "SELECT DeviceID, Name, Speed FROM Win32_NetworkAdapter WHERE PhysicalAdapter = TRUE", &rows, warnings) {
		return
	}
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" || ignoredPhysicalAdapter(name) {
			continue
		}
		info.PhysicalNICs = append(info.PhysicalNICs, PhysicalNICInfo{
			DeviceID:  strings.TrimSpace(row.DeviceID),
			Name:      name,
			SpeedMbps: formatSpeedMbps(row.Speed),
		})
	}
}

func ignoredPhysicalAdapter(name string) bool {
	if ignoredAdapter(name) {
		return true
	}
	lowerName := strings.ToLower(name)
	for _, keyword := range []string{"virtual", "vmware", "hyper-v"} {
		if strings.Contains(lowerName, keyword) {
			return true
		}
	}
	return false
}

func collectBatteries(info *HardwareInfo, warnings *[]string) {
	var rows []win32Battery
	if !queryHardware("电池信息", "SELECT DeviceID, Name, EstimatedChargeRemaining, BatteryStatus, DesignCapacity, FullChargeCapacity FROM Win32_Battery", &rows, warnings) {
		return
	}
	for _, row := range rows {
		battery := BatteryInfo{
			DeviceID:              strings.TrimSpace(row.DeviceID),
			Name:                  strings.TrimSpace(row.Name),
			ChargePercent:         formatOptionalUint16(row.EstimatedChargeRemaining),
			Status:                batteryStatusName(row.BatteryStatus),
			DesignCapacityMWh:     formatOptionalUint32(row.DesignCapacity),
			FullChargeCapacityMWh: formatOptionalUint32(row.FullChargeCapacity),
		}
		if battery.DeviceID == "" && battery.Name == "" {
			continue
		}
		info.Batteries = append(info.Batteries, battery)
	}
}

func queryHardware(label, query string, dst any, warnings *[]string) bool {
	return runHardwareQuery(label, query, dst, warnings)
}

func queryHardwareNamespace(label, query, namespace string, dst any, warnings *[]string) bool {
	return runHardwareQuery(label, query, dst, warnings, nil, namespace)
}

// wmi v1.2.4 转换异常 SAFEARRAY 时可能 panic，将其限制在当前硬件查询内。
func runHardwareQuery(label, query string, dst any, warnings *[]string, connectArgs ...any) (ok bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			*warnings = append(*warnings, "读取"+label+"失败: "+fmt.Sprint(recovered))
			ok = false
		}
	}()
	if err := hardwareWMI.Query(query, dst, connectArgs...); err != nil {
		*warnings = append(*warnings, "读取"+label+"失败: "+err.Error())
		return false
	}
	return true
}

func sortHardware(info *HardwareInfo) {
	sort.Strings(info.CPUSockets)
	sort.SliceStable(info.MemoryModules, func(i, j int) bool {
		return info.MemoryModules[i].Slot < info.MemoryModules[j].Slot
	})
	sort.SliceStable(info.Disks, func(i, j int) bool { return info.Disks[i].DeviceID < info.Disks[j].DeviceID })
	sort.SliceStable(info.Volumes, func(i, j int) bool { return info.Volumes[i].DeviceID < info.Volumes[j].DeviceID })
	sort.SliceStable(info.Monitors, func(i, j int) bool { return info.Monitors[i].InstanceName < info.Monitors[j].InstanceName })
	sort.SliceStable(info.SoundDevices, func(i, j int) bool {
		return info.SoundDevices[i].PNPDeviceID < info.SoundDevices[j].PNPDeviceID
	})
	sort.SliceStable(info.PhysicalNICs, func(i, j int) bool {
		return info.PhysicalNICs[i].DeviceID < info.PhysicalNICs[j].DeviceID
	})
	sort.SliceStable(info.Batteries, func(i, j int) bool {
		return info.Batteries[i].DeviceID < info.Batteries[j].DeviceID
	})
}

var hardwareCSVHeader = []string{
	"电脑品牌", "整机序列号", "硬件UUID", "机箱类型", "资产标签",
	"主板厂商", "主板型号", "主板版本", "主板序列号", "BIOS厂商", "BIOS版本", "BIOS发布日期",
	"CPU插槽", "内存插槽", "内存厂商", "内存料号",
	"物理硬盘型号", "物理硬盘序列号", "物理硬盘容量(GB)", "物理硬盘接口", "物理硬盘介质",
	"逻辑分区盘符", "逻辑分区卷标", "逻辑分区文件系统", "逻辑分区容量(GB)", "逻辑分区可用空间(GB)",
	"显示器厂商", "显示器型号", "显示器序列号",
	"声卡名称", "声卡厂商",
	"物理网卡名称", "物理网卡速率(Mbps)",
	"电池名称", "电池电量(%)", "电池状态", "电池设计容量(mWh)", "电池满充容量(mWh)",
}

func hardwareRecord(info *DeviceInfo) []string {
	h := &info.Hardware
	return []string{
		h.Manufacturer,
		h.SerialNumber,
		h.UUID,
		h.ChassisType,
		h.AssetTag,
		h.BaseboardManufacturer,
		h.BaseboardModel,
		h.BaseboardVersion,
		h.BaseboardSerialNumber,
		h.BIOSManufacturer,
		h.BIOSVersion,
		h.BIOSReleaseDate,
		hardwareCells(h.CPUSockets, func(v string) string { return v }),
		hardwareCells(h.MemoryModules, func(v MemoryModuleInfo) string { return v.Slot }),
		hardwareCells(h.MemoryModules, func(v MemoryModuleInfo) string { return v.Manufacturer }),
		hardwareCells(h.MemoryModules, func(v MemoryModuleInfo) string { return v.PartNumber }),
		hardwareCells(h.Disks, func(v DiskInfo) string { return v.Model }),
		hardwareCells(h.Disks, func(v DiskInfo) string { return v.SerialNumber }),
		hardwareCells(h.Disks, func(v DiskInfo) string { return v.SizeGB }),
		hardwareCells(h.Disks, func(v DiskInfo) string { return v.InterfaceType }),
		hardwareCells(h.Disks, func(v DiskInfo) string { return v.MediaType }),
		hardwareCells(h.Volumes, func(v VolumeInfo) string { return v.DeviceID }),
		hardwareCells(h.Volumes, func(v VolumeInfo) string { return v.VolumeName }),
		hardwareCells(h.Volumes, func(v VolumeInfo) string { return v.FileSystem }),
		hardwareCells(h.Volumes, func(v VolumeInfo) string { return v.SizeGB }),
		hardwareCells(h.Volumes, func(v VolumeInfo) string { return v.FreeGB }),
		hardwareCells(h.Monitors, func(v MonitorInfo) string { return v.Manufacturer }),
		hardwareCells(h.Monitors, func(v MonitorInfo) string { return v.Model }),
		hardwareCells(h.Monitors, func(v MonitorInfo) string { return v.SerialNumber }),
		hardwareCells(h.SoundDevices, func(v SoundDeviceInfo) string { return v.Name }),
		hardwareCells(h.SoundDevices, func(v SoundDeviceInfo) string { return v.Manufacturer }),
		hardwareCells(h.PhysicalNICs, func(v PhysicalNICInfo) string { return v.Name }),
		hardwareCells(h.PhysicalNICs, func(v PhysicalNICInfo) string { return v.SpeedMbps }),
		hardwareCells(h.Batteries, func(v BatteryInfo) string { return v.Name }),
		hardwareCells(h.Batteries, func(v BatteryInfo) string { return v.ChargePercent }),
		hardwareCells(h.Batteries, func(v BatteryInfo) string { return v.Status }),
		hardwareCells(h.Batteries, func(v BatteryInfo) string { return v.DesignCapacityMWh }),
		hardwareCells(h.Batteries, func(v BatteryInfo) string { return v.FullChargeCapacityMWh }),
	}
}

func hardwareCells[T any](items []T, get func(T) string) string {
	if len(items) == 0 {
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		value := strings.TrimSpace(get(item))
		// 原始料号等文本可能含 /，改为全角以免与多设备分隔符混淆。
		value = strings.ReplaceAll(value, "/", "／")
		if value == "" {
			value = "-"
		}
		parts = append(parts, value)
	}
	return strings.Join(parts, "/")
}

func formatBytesGB(value *uint64) string {
	if value == nil || *value == 0 {
		return ""
	}
	return strconv.FormatFloat(float64(*value)/(1024*1024*1024), 'f', 1, 64)
}

func formatBytesGBAllowZero(value *uint64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatFloat(float64(*value)/(1024*1024*1024), 'f', 1, 64)
}

func formatSpeedMbps(value *uint64) string {
	// 部分断开的网卡用有符号/无符号整数最大值表示未知速率。
	if value == nil || *value == 0 || *value == uint64(1<<63-1) || *value == ^uint64(0) {
		return ""
	}
	return strconv.FormatFloat(float64(*value)/1000000, 'f', 0, 64)
}

func formatOptionalUint16(value *uint16) string {
	if value == nil {
		return ""
	}
	return strconv.FormatUint(uint64(*value), 10)
}

func formatOptionalUint32(value *uint32) string {
	if value == nil {
		return ""
	}
	return strconv.FormatUint(uint64(*value), 10)
}

func formatWMIDate(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 8 {
		if _, err := time.Parse("20060102", value[:8]); err == nil {
			return value[:4] + "-" + value[4:6] + "-" + value[6:8]
		}
	}
	return value
}

func decodeWMIString(values []int32) string {
	units := make([]uint16, 0, len(values))
	for _, value := range values {
		if value == 0 {
			break
		}
		units = append(units, uint16(value))
	}
	return strings.TrimSpace(string(utf16.Decode(units)))
}

func chassisTypeName(code int32) string {
	switch code {
	case 1:
		return "其他"
	case 2:
		return "未知"
	case 3:
		return "台式机"
	case 4:
		return "低矮台式机"
	case 6:
		return "迷你塔式"
	case 7:
		return "塔式"
	case 8:
		return "便携机"
	case 9, 10:
		return "笔记本"
	case 11:
		return "手持设备"
	case 12:
		return "扩展坞"
	case 13:
		return "一体机"
	case 14:
		return "小型笔记本"
	case 15:
		return "节省空间型"
	case 17:
		return "主机箱"
	case 18:
		return "扩展机箱"
	case 23:
		return "机架式"
	case 24:
		return "密封式电脑"
	case 28:
		return "刀片服务器"
	case 29:
		return "刀片机箱"
	case 30:
		return "平板电脑"
	case 31:
		return "二合一电脑"
	case 32:
		return "可拆卸电脑"
	case 33:
		return "物联网网关"
	case 34:
		return "嵌入式电脑"
	case 35:
		return "迷你电脑"
	case 36:
		return "计算棒"
	default:
		return strconv.FormatInt(int64(code), 10)
	}
}

func batteryStatusName(code *uint16) string {
	if code == nil {
		return ""
	}
	switch *code {
	case 1:
		return "放电中"
	case 2:
		return "使用交流电源"
	case 3:
		return "已充满"
	case 4:
		return "电量低"
	case 5:
		return "电量严重低"
	case 6:
		return "充电中"
	case 7:
		return "充电中（电量高）"
	case 8:
		return "充电中（电量低）"
	case 9:
		return "充电中（电量严重低）"
	case 10:
		return "未定义"
	case 11:
		return "部分充电"
	default:
		return strconv.FormatUint(uint64(*code), 10)
	}
}
