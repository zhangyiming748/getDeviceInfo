# getDeviceInfo
获取资产信息，建立计算机台账
## 说明
我希望的是最终生成一个完整CSV逗号分隔文件，其中包括计算机和相关设备的信息
必须的信息有 电脑名称 hostname ip地址（全部网卡） mac地址（全部网卡） cpu型号 cpu核心数 cpu频率 内存大小 内存类型 内存频率 显卡型号 显卡显存 显卡频率 显卡内存类型 显卡内存频率
可选的信息有 连接的打印机的 名称ip地址 mac地址

CSV 默认输出到当前用户桌面（文件名 `设备台账_<电脑名>_<时间>.csv`）。

## 输出列

| 列名 | 说明 |
| --- | --- |
| 电脑名称 | `Win32_ComputerSystem.Name` |
| Hostname | `os.Hostname()`（为空时取网卡 DNS 主机名） |
| IP地址 | 所有已启用网卡的 IPv4，其后为全局 IPv6，`/` 分隔；排除 127.x、169.254.x、fe80:: 等 |
| MAC地址 | 所有网卡（排除 WAN Miniport、蓝牙 PAN 等虚拟/隧道网卡），`/` 分隔 |
| CPU型号 | 物理 CPU 型号，多路时 `/` 分隔 |
| CPU核心数 / CPU线程数 | 物理核心数 / 逻辑处理器数 |
| CPU频率(MHz) | 最大主频（`MaxClockSpeed`） |
| 内存大小(GB) | 所有内存条容量之和（GiB） |
| 内存类型 / 内存频率(MHz) | 由 SMBIOS 内存类型码换算（DDR3/DDR4/DDR5…），多规格时去重后 `/` 分隔 |
| 显卡型号 / 显卡显存(MB) / 显卡内存类型 | 多显卡按顺序 `/` 分隔；显存取驱动注册表 `qwMemorySize`（不受 WMI AdapterRAM 4GB 溢出影响） |
| 显卡频率(MHz) / 显卡内存频率(MHz) | 仅 NVIDIA 可通过 `nvidia-smi` 获取，其他显卡为 `-` |
| 打印机名称 / IP地址 / MAC地址 | 本机已安装的打印机；IP 来自 TCP/IP 端口或 `IP_`、共享端口解析；MAC 通过 ARP 查询 |
| 打印机端口 | WSD 端口 / TCP/IP 端口 / USB 端口 / 共享打印机端口等 |

同一单元格内的多项数据统一使用 `/` 分隔；显卡列缺失值用 `-` 占位以保持与型号列对齐。

## 构建方法（忘了就照这里做）

### 1. 准备环境（只需一次）

- 安装 Go 1.26 或更高版本：`winget install GoLang.Go`（或到 https://go.dev/dl/ 下载 msi），装完重开终端确认：
  ```powershell
  go version
  ```
- 首次编译需要联网拉取 3 个依赖（`yusufpapurcu/wmi`、`go-ole/go-ole`、`golang.org/x/sys`）。国内网络建议先设置代理：
  ```powershell
  go env -w GOPROXY=https://goproxy.cn,direct
  ```

### 2. 编译

```powershell
cd C:\Users\zen\Github\getDeviceInfo
go mod download
go build -trimpath -ldflags "-s -w -H windowsgui" -o getDeviceInfo.exe .
```

> 末尾的 `.` 表示“编译当前目录下的整个包”，会把 `main.go`、`collect.go`、`gpu.go`、`printer.go` 一起编进去。
> 不要写成 `go build main.go`——那只会编译单个文件，会报 `undefined: collectDeviceInfo` 之类的错误。
> 也可以直接用不带参数的 `go build -o getDeviceInfo.exe`（在模块根目录时等价）。
>
> `-H windowsgui` 让 exe 以图形界面方式启动：**双击不会弹出黑色命令行窗口**，采集完成后弹出提示框，可直接点“是”打开文件所在位置。
> 去掉这个参数也能编译（控制台版），只是双击时会先闪一个命令行窗口。

产物 `getDeviceInfo.exe` 约 2.8 MB，单文件、纯静态，无需 .NET / Python / VC++ 运行时，直接拷到其他机器即可运行。

### 3. 编译后自检

```powershell
.\getDeviceInfo.exe -out .\test.csv
Get-Content .\test.csv
```

运行后应弹出“设备信息采集完成”提示框（点“否”即可），同时生成 `test.csv`，能看到包含本机信息的一行 CSV 即为成功（自检完可删掉 `test.csv`）。

### 4. 其他架构（按需）

```powershell
$env:GOARCH="386";   go build -trimpath -ldflags "-s -w -H windowsgui" -o getDeviceInfo_x86.exe .;   $env:GOARCH="amd64"
$env:GOARCH="arm64"; go build -trimpath -ldflags "-s -w -H windowsgui" -o getDeviceInfo_arm64.exe .; $env:GOARCH="amd64"
```

支持 Windows 10 / 11、Windows Server 2016 及以上（Go 1.26 不再支持 Win7/8）。

## 使用方法

### 单机采集（交付给非计算机人员）

把 `getDeviceInfo.exe` 发给使用者，**双击即可**，无需打开命令行、无需管理员权限：

1. 双击 `getDeviceInfo.exe`（不会出现黑色命令行窗口）；
2. 等待 1~2 秒，桌面出现 `设备台账_<电脑名>_<时间>.csv`；
3. 弹出“设备信息采集完成”提示框，点“是”可直接定位到该文件，点“否”关闭。

也可以在命令行里指定输出位置：

```powershell
.\getDeviceInfo.exe                              # 保存到当前用户桌面
.\getDeviceInfo.exe -out "D:\资产台账\$(hostname).csv"   # 指定路径，缺省扩展名自动补 .csv
```

- 桌面路径通过注册表读取，OneDrive 重定向过桌面也能正确保存。
- 默认文件名：`设备台账_<电脑名>_<年月日_时分秒>.csv`。
- 文件编码 UTF-8（带 BOM）、行尾 CRLF，Excel 双击打开中文不乱码。

### 多机汇总

把各机器生成的 CSV 收集到同一目录，再合并成一张总表（只保留第一份表头）：

```powershell
$files = Get-ChildItem "D:\资产台账\收集\*.csv"
$all   = New-Object System.Collections.Generic.List[string]
$first = $true
foreach ($f in $files) {
    $lines = [string[]](Get-Content $f.FullName -Encoding UTF8)
    if ($first) { $all.AddRange($lines); $first = $false }
    elseif ($lines.Count -gt 1) { $all.AddRange([string[]]$lines[1..($lines.Count-1)]) }
}
$all | Set-Content -Path "D:\资产台账\汇总.csv" -Encoding utf8BOM
```

### 参数说明

| 参数 | 说明 |
| --- | --- |
| `-out <路径>` | 指定输出文件，可带目录；不指定时保存到桌面 |
| `-h` | 显示帮助 |

## 数据来源与限制

- CPU、内存、网卡、显卡型号、打印机均通过 WMI（`root\cimv2`）读取，需要 WMI 服务（`winmgmt`）正常。
- 显存大小优先读注册表 `HKLM\SYSTEM\CurrentControlSet\Control\Class\{4d36e968-...}\xxxx\HardwareInformation.qwMemorySize`；集成显卡无独立显存，显存列显示 `-`、类型显示“共享内存”。
- Windows 不提供显存颗粒类型（GDDR5/GDDR6 等）的 API，独立显卡统一标注为 `GDDR`；显存频率只能从 NVIDIA 驱动自带的 `nvidia-smi` 读取，机器未安装 NVIDIA 驱动工具时为空。
- WSD / IPP 端口的打印机，Windows 不保存其 IP 地址，因此这类打印机的 IP、MAC 列为空；使用标准 TCP/IP 端口或共享端口的打印机可正常解析。
- 打印机 MAC 通过 ARP（`SendARP`）获取，仅对与本机同网段且在线的打印机有效。

## 常见问题

| 现象 | 原因 / 处理 |
| --- | --- |
| 双击后什么都不发生 | 若 exe 未用 `-H windowsgui` 编译，会先弹出命令行窗口再采集；若提示框也未出现，检查任务管理器里是否有进程卡住，或改用命令行运行查看报错 |
| 双击被 Windows 拦截（“Windows 已保护你的电脑”） | 未签名 exe 触发了 SmartScreen，点“更多信息 → 仍要运行”，或把 exe 加白名单；内网分发可让 IT 签名 |
| 找不到生成的 CSV | 默认在桌面（含 OneDrive 重定向目录，如 `C:\Users\<用户>\OneDrive\桌面`）；用 `-out` 指定路径最稳妥 |
| Excel 打开中文乱码 | 程序已写入 UTF-8 BOM，请用 Excel 直接双击；不要用记事本“另存为 ANSI” |
| 打印机 IP/MAC 为空 | 该打印机是 WSD/IPP 端口（Windows 不保存 IP）；或打印机跨网段/离线导致 ARP 查不到 |
| 显卡频率、显存频率为 `-` | 非 NVIDIA 显卡，或未安装 NVIDIA 驱动工具 `nvidia-smi` |
| 集显显存为 `-` | 集成显卡共享系统内存，没有独立显存值 |
| 某些字段为空 | 多为虚拟机/精简系统缺少对应 WMI 数据；用命令行方式运行会输出“警告: 读取 xxx 失败”便于排查 |
| 编译报 `go: cannot find module` | 未联网或代理未生效，执行第 1 步的 `GOPROXY` 设置后重试 `go mod download` |
| 杀毒软件拦截 exe | 静态单文件 exe 偶被误报，可自行 `go build` 或加白名单 |

