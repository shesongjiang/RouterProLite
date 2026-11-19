# Pro-Lite

轻量级 OpenWrt / ImmortalWrt 路由器桌面助手。采用 Go + Wails（系统 WebView），单文件二进制，常驻内存远低于 Electron 方案。

- 窗口：420×640 无边框弹出式面板，14px 圆角，深色 / 浅色双主题
- 连接：SSH（默认 `root@192.168.100.1:22`），支持 免密(none)、SSH Agent、私钥、密码四种认证
- 数据：全部来自路由器真实命令输出（ubus / procfs / init.d / iwinfo / apk / opkg），不做伪造遥测
- 数据中心：主面板之外提供完整“控制中心”，日常管理无需再打开 LuCI
- 平台：Windows 10+、macOS 11+、Linux（WebKitGTK）

## 功能

**主面板**
- WAN 状态：协议、状态、IP / 网关 / DNS、复制 IP、延迟、在线时长、链路速率
- 2.5D 设备拓扑：等距路由器机身 + WAN / WAN6 / LAN / WiFi / 蜂窝端口，真实状态着色、UP 链路流动动画、点击端口直达对应接口；保留 RX / TX 实时速率
- 硬件：CPU / 内存 / 温度（无温度传感器的设备自动隐藏温度条）
- 硬件与设备卡：CPU / 内存 / 温度、型号、芯片、内核、发行版、根分区；可点击插件芯片直达控制中心
- 策略磁贴：按设备能力动态生成（WAN、防火墙 / FullCone、DHCP / DNS、代理 / AdGuard / SQM / MWAN3 等）
- 终端列表：DHCP 租约 + ARP 合并，在线状态、租约剩余时间、静态绑定 / 黑名单标记；行内抽屉可设静态租约、拉黑 / 解黑
- 3 秒二次确认：所有危险操作（重启、断开 WAN、拉黑、删除规则）需连续确认

**控制中心（Ctrl / Cmd + K，两个标签页）**

控制中心 · 系统与配置：
- 概览：设备型号 / 芯片 / 固件 / 内核 / 架构 / CPU 频率 / 内存详情 / 存储占用 / 运行时长
- 系统：主机名、时区、时区名、NTP 服务器（写入 `/etc/config/system` 并重载服务）
- 接口：全部逻辑接口的协议、设备、IP、网关、DNS、流量计数，支持 up / renew / down
- 无线：射频与 SSID 列表、信道 / 频宽 / 发射功率、加密方式与密码修改（无无线硬件的设备自动提示）
- 插件：已安装且运行中的扩展服务（MWAN3、uHTTPd、ttyd、AdGuard、代理等）
- 软件包：读取本机包数据库（apk / opkg），支持搜索，显示包名与版本
- DHCP：地址池启停 / 起始偏移 / 数量 / 租期、静态租约、活动租约与一键拉黑
- 防火墙：区域转发矩阵、规则数、端口映射、UPnP / NAT-PMP 状态与开关
- 配置：直接浏览并编辑任意 UCI 配置（标量与列表），支持新增 / 删除选项与 commit
- 日志：`logread` / `dmesg`，按关键词过滤与行数限制
- 进程与存储：`ps` 进程列表、`df` 与挂载点

工具（原有高级实验室）：
- 端口转发：列表、启停、删除、新增（带端口 / IPv4 校验）
- 网络探测：Ping / Traceroute（结果原样回显）
- DNS 服务器与 FullCone NAT 开关
- DDNS 服务状态
- 服务启停：SQM、AdGuard Home、DoH、PassWall / ShellCrash / OpenClash / Clash、MWAN3
- SSH 终端：在路由器上执行任意命令（25s 超时，2000 字符上限）
- 链路 / 硬件详情、重启路由器、重载网络

## 快速开始

1. 下载或自行构建对应平台的二进制。
2. 启动后填写主机（默认 `192.168.100.1`）、端口、用户。
3. 选择认证方式：
   - **免密（none）**：路由器允许空认证时使用，例如已放行的 root 会话
   - **Agent**：使用系统 SSH Agent（Windows OpenSSH Agent 命名管道 / Unix `SSH_AUTH_SOCK`）
   - **私钥**：手动选择私钥文件，可填 passphrase
   - **密码**：仅在本次连接中使用
4. 首次连接会显示主机密钥 SHA256 指纹，确认后固定（TOFU）。之后指纹变化会拒绝连接，可在连接页“重置指纹”。

## 安全说明

- 密码**不会**写入磁盘；仅当勾选“记住配置”时保存主机、端口、用户、认证方式与私钥路径。
- 私钥仅在用户通过文件选择器指定后读取，程序不会自动扫描 `~/.ssh`。
- 主机密钥采用 TOFU 固定：首次确认后写入配置，后续不匹配即中止连接，防止中间人。
- 所有写操作先经参数校验（MAC / IPv4 / 端口 / 包名白名单），通过 ubus UCI 提交后再 reload 对应服务。
- 配置与日志位于 `os.UserConfigDir()/ProLite/`（Windows `%AppData%\ProLite`，macOS `~/Library/Application Support/ProLite`，Linux `~/.config/ProLite`）。日志超过 2MB 自动轮转。

## 为什么是 Go + Wails

| 方案 | 常驻内存 | 体积 | 说明 |
| --- | --- | --- | --- |
| Electron | 150MB+ | 100MB+ | 自带 Chromium |
| Go + Wails | ~20–40MB | 单文件 ~18MB | 复用系统 WebView，Go 进程本身很小 |
| 纯 Qt | ~30MB | 需 Qt 运行库 | 打包分发复杂 |

Wails 在 Windows 使用 WebView2（系统自带），macOS 使用 WKWebView，Linux 使用 WebKitGTK，三者均无需随包分发浏览器内核。

## 构建

依赖 Go 1.27.4+（与 go.mod 保持一致）。Wails 资源已通过 `//go:embed` 内嵌，**无需安装 Wails CLI**。

### Windows

```powershell
# 需要 WebView2 Runtime（Win10/11 通常已内置）
go build -tags desktop,production -ldflags "-H windowsgui -s -w" -o ProLite.exe .
```

### macOS

```bash
xcode-select --install   # 需要 Command Line Tools
go build -tags desktop,production -ldflags "-s -w" -o ProLite .
```

### Linux

```bash
# Debian / Ubuntu 24.04+（webkit2gtk-4.1）
sudo apt install build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev
go build -tags desktop,production,webkit2_41 -ldflags "-s -w" -o ProLite .

# Ubuntu 22.04 等仅提供 webkit2gtk-4.0 的发行版
sudo apt install build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.0-dev
go build -tags desktop,production -ldflags "-s -w" -o ProLite .
```

或使用脚本：

```bash
./scripts/build.sh          # macOS / Linux
```

```powershell
.\scripts\build.ps1         # Windows
```

### 测试

```bash
go vet ./...
go test ./...                 # 离线单测（apk/opkg、CPU、UCI、DHCP、遥测解析等）

# 真机只读验证：用应用自身的 SSH 与采集代码连接 192.168.100.1，
# 读取 board / 系统 / 接口 / 无线 / 插件 / 包 / 进程 / 存储 / 日志 / UCI，
# 只读，不写入任何路由器配置。
PROLITE_LIVE=1 go test -run TestLiveConsole -v -count=1
```

实测设备（2026-11-19）：ImmortalWrt SNAPSHOT r39235，Linux 6.18.26，MediaTek MT7621（4 核 @880MHz），MelonPi M21 16M，241MB 内存，apk 包管理器；采集到 4 个接口、284 个软件包、19 份 UCI 配置、3 个 DHCP 池、2 个防火墙区域 / 9 条规则，耗时约 3.5s。

## 界面预览（无需路由器）

浏览器打开 `frontend/index.html?mock=1`（需通过本地 HTTP 服务，`file://` 下查询参数会被丢弃）：

```bash
cd frontend && python3 -m http.server 8099
# http://127.0.0.1:8099/index.html?mock=1
```

`mock=1` 使用内置假数据，仅用于界面调试；真实程序始终走 SSH。

## 目录结构

```
main.go            Wails 启动、窗口参数、日志
app.go             前端绑定的全部方法
router.go          SSH 连接管理、快照采集、实验室数据
ssh.go             SSH 认证 / TOFU 主机密钥校验
telemetry.go       /proc 与 ubus 解析、客户端合并、速率计算
meta.go            设备能力探测、UCI 配置解析
console.go         控制中心采集（接口 / 无线 / 插件 / 包 / 进程 / 存储 / DHCP / 防火墙）
settings.go        系统 / 无线 / DHCP / UPnP / 接口 / UCI 写入与校验
actions.go         端口转发 / 租约 / 黑名单 / DNS / 服务 / WAN 操作
tiles.go           能力驱动的磁贴与实验室数据
types.go           前后端 JSON 模型
live_verify_test.go 真机只读验证（PROLITE_LIVE=1）
frontend/          原生 HTML / CSS / JS 界面
scripts/           跨平台构建脚本
```

## 已知限制

- 本次交付对 192.168.100.1 的验证为**只读**：采集、解析与界面数据均来自真实设备；写入路径已通过编译、参数校验与 mock 交互测试，但未对实机执行配置写入。
- 每客户端限速：当前固件无统一的 per-client 整形接口，界面仅显示说明，不写入 tc 规则。
- 客户端实时速率：依赖设备侧逐客户端统计，多数 OpenWrt 固件不提供，界面回退显示租约 / 静态信息。
- 无无线硬件的设备（如当前 MelonPi M21）无线页会显示空状态，公共 UCI 配置仍可正常编辑。
- 需要路由器开启 SSH 且当前用户有 root 权限（UCI 写操作）。
