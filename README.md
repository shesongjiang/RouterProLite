# ProLite

轻量级 OpenWrt / ImmortalWrt 路由器桌面助手。使用 Go + Wails v2（系统 WebView）构建，单文件二进制，通过 SSH 读取和管理真实设备，不依赖 LuCI 之外的服务端组件。

- 平台：Windows 10/11（WebView2）、macOS 11+、Linux（WebKitGTK）
- 连接：SSH，默认 `root@192.168.100.1:22`
- 数据来源：路由器真实命令输出（ubus / procfs / init.d / iwinfo / apk / opkg / UCI）
- 本仓库同时包含源码、Windows 发布包与校验文件

## 主要功能

- **主面板**：WAN 状态、2.5D 设备拓扑、CPU/内存/温度、系统与固件信息、终端列表（DHCP 租约 + ARP）、按设备能力生成的快捷磁贴
- **控制中心**（Ctrl / Cmd + K）：概览、系统、接口、无线、插件、软件包、DHCP、防火墙、UCI 配置、日志、进程与存储
- **工具**：端口转发、Ping / Traceroute、DNS、FullCone NAT、DDNS、常用服务启停、SSH 终端、重启与重载网络
- **插件市场**：浏览精选 noarch 插件，依赖求解、空间预检、下载校验、设备端预演，确认后真实安装

## 快速开始（使用发布包）

1. 运行 `02-bin/ProLite.exe`（Windows 10/11 x64）。
2. 系统需要 **Microsoft Edge WebView2 Runtime**（Win11 通常自带；缺失时从微软官网安装）。
3. 填写主机、端口、用户，选择认证方式后连接。
4. 首次连接会显示主机密钥 SHA256 指纹，确认后固定（TOFU）；之后指纹变化会拒绝连接，可在连接页重置指纹。

程序为单文件，无需安装 Go、Wails 或 Node。

## 从源码构建

依赖 **Go 1.27.4+**。前端资源已通过 `//go:embed` 内嵌，**不需要安装 Wails CLI**。

### Windows

```powershell
go build -tags desktop,production -ldflags "-H windowsgui -s -w" -o ProLite.exe .
```

### macOS

```bash
xcode-select --install
go build -tags desktop,production -ldflags "-s -w" -o ProLite .
```

### Linux

```bash
# Debian / Ubuntu 24.04+（webkit2gtk-4.1）
sudo apt install build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev
go build -tags desktop,production,webkit2_41 -ldflags "-s -w" -o ProLite .

# 仅提供 webkit2gtk-4.0 的发行版（如 Ubuntu 22.04）
sudo apt install build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.0-dev
go build -tags desktop,production -ldflags "-s -w" -o ProLite .
```

或使用脚本：

```powershell
.\scripts\build.ps1          # Windows
```

```bash
./scripts/build.sh           # macOS / Linux
```

### 测试

```bash
go vet ./...
go test ./...                # 离线单测：包元数据、依赖求解、市场约束、配置写入校验、遥测解析等
```

真机只读验证（需要能连到设备；不会写入配置）：

```bash
PROLITE_LIVE=1 go test -run TestLiveConsole -v -count=1
```

真机安装/回滚闭环（会真实写入设备，默认跳过，需显式双重开关）：

```bash
PROLITE_LIVE=1 PROLITE_INSTALL=1 PROLITE_MARKET_ID=uhttpd \
  go test -run TestLiveMarketInstallRollback -v -count=1
```

## 连接设备

- 默认主机 `192.168.100.1`、端口 `22`、用户 `root`。
- 认证方式：
  - `none`：设备允许空认证时使用
  - `agent`：系统 SSH Agent（Windows OpenSSH Agent 命名管道 / Unix `SSH_AUTH_SOCK`）
  - `key`：选择私钥文件，可填 passphrase
  - `password`：仅本次连接使用，不落盘
- 勾选“记住配置”时只保存主机、端口、用户、认证方式、私钥路径与固定指纹。
- 配置与日志位于 `os.UserConfigDir()/ProLite/`：
  - Windows：`%AppData%\ProLite\`
  - macOS：`~/Library/Application Support/ProLite/`
  - Linux：`~/.config/ProLite/`
- 日志为 `prolite.log`，超过 2 MB 自动轮转。

## 插件市场

### 入口

连接设备后按 Ctrl / Cmd + K 打开控制中心，第二个主标签为“插件市场”。未连接时也可从启动页的“插件市场（离线浏览）”进入，但只能浏览，不能预演或安装。

### 目录与数据来源

- 目录是内置精选清单（12 个 LuCI / 脚本类插件），随程序版本更新，不依赖远程动态清单。
- 包版本、依赖、下载体积、安装后体积、已安装状态，全部在预演时从**设备自身的包源**实时解析。
- 支持的源：
  - apk：`/etc/apk/repositories`、`/etc/apk/repositories.d/*`
  - opkg：`/etc/opkg/distfeeds.conf`
- 索引格式：apk v3 `packages.adb`（ADB/ADBd）、旧版 `APKINDEX.tar.gz`、opkg `Packages.gz`。

### 安装流程

1. 刷新市场，选中插件，点击预演。
2. 程序拉取索引并求解依赖闭包，执行市场约束与空间预检。
3. 预演阶段依次完成：下载 → 本地 SHA256 校验 → 上传到 `/tmp/prolite-<job>/` → 远端 SHA256 复核 → 设备端 `apk add --simulate`（opkg 固件为 `opkg install --noaction`）。
4. 预演通过后，在确认区逐字输入当前设备地址、勾选写入确认，才允许真实安装。
5. 真实安装前记录包列表、`/etc/init.d` 与配置哈希快照，逐包安装并回读比对。
6. 程序**不自动卸载**。需要回滚时，在设备上执行 `apk del <包名>` 或 `opkg remove <包名>`。

上传目录固定为 `/tmp/prolite-<job>/`，任务结束会删除；只使用已上传的本地包，不修改设备 feed 配置，也不执行 `apk update` / `opkg update`。

### 硬性限制

| 限制 | 规则 |
| --- | --- |
| 包类型 | 只允许 `noarch` / `all` 的脚本与 LuCI 包 |
| 禁止包 | `kmod-*`、`kernel`、`linux-*`、`uboot*`、`base-files`、`libc`、`busybox`、`dropbear`、`procd`、`netifd`、`firewall4`、`fstools`、`mtd`、`apk`、`opkg` 等 |
| 单包体积 | 下载或安装后体积不超过 **4 MiB** |
| 整单体积 | 安装后总体积不超过 **12 MiB** |
| 空间 | overlay 与 `/tmp` 必须容纳依赖闭包与上传文件 |
| 依赖 | 依赖闭包必须能在设备已配置的源中完整解析 |

任一条件不满足都会在预演阶段失败并给出原因，不会下载后硬装。程序不使用 `--force`；包签名默认由设备 apk/opkg 原生校验，只有设备明确返回 `UNTRUSTED` 且该文件已在桌面端通过 SHA256 校验时，才会对单个文件使用 `--allow-untrusted` 重试。

### 真实写入授权文件

真实安装默认禁止，必须在桌面端创建：

`%AppData%\ProLite\device-write-allow.json`（macOS / Linux 对应 `os.UserConfigDir()/ProLite/`）

```json
{
  "host": "192.168.100.1",
  "allowOverlay": true,
  "allowTmp": true,
  "allowDangerous": false,
  "note": "仅用于我的测试路由器"
}
```

- `host` 必须与当前连接设备完全一致。
- `allowOverlay` 与 `allowTmp` 必须同时为 `true`。
- 危险包同时需要授权文件与界面勾选；插件市场本身不会放行危险包。
- 文件不存在或校验失败时，真实安装一律拒绝。

### 已知限制

- `luci-app-sqm` 依赖 `kmod-ifb`、`kmod-sched-core` 等内核模块。设备未预装这些模块时依赖无法满足，且 kmod 属于禁止范围，因此 SQM 通常无法通过市场安装。
- `luci-app-opkg` / `luci-app-filetransfer` 等条目在部分 apk snapshot 源中不存在，预演会在依赖求解阶段提示无法解析。
- 市场只覆盖内置精选清单，不是完整包管理器；任意包安装请使用“安装”页的高级模式。

## 仓库目录结构

```text
01-source/router-prolite/    Go 源码、前端与文档
  frontend/                  原生 HTML / CSS / JS（Wails 内嵌）
  docs/                      运行说明与安装流水线设计
  scripts/                   跨平台构建脚本
  build/                     本地构建输出（.gitignore 排除）
02-bin/                      Windows 发布包 ProLite.exe 与运行说明
03-docs/                     随发布包分发的文档
04-checksums/                SHA256SUMS.txt、BUILD-INFO.txt
```

## 校验文件怎么用

`04-checksums/SHA256SUMS.txt` 每行格式为 `<sha256>  <相对仓库根目录的路径>`，覆盖 `01-source`（不含 `build/`）、`02-bin` 与 `03-docs`。`04-checksums/BUILD-INFO.txt` 记录构建时间、Go 版本、发布包大小、SHA256 与源码文件数。

Windows PowerShell 示例：

```powershell
(Get-FileHash .\02-bin\ProLite.exe -Algorithm SHA256).Hash.ToLower()
Select-String -Path .\04-checksums\SHA256SUMS.txt -Pattern '02-bin\\ProLite.exe'
```

两处哈希一致即可。也可以逐条核对清单中的其它文件。

## 当前验证程度

已在本机真实设备上验证（2026-11-19）：

- 设备：MT7621 / MelonPi M21，ImmortalWrt SNAPSHOT，`mipsel_24kc`，apk-tools 3.0.5，约 290 个已装包
- SSH 免密（none）连接、TOFU 指纹固定
- 只读采集：系统、接口、无线、插件、软件包、进程、存储、日志、UCI
- 包环境读取：包管理器、架构、内核、8 个 feed、overlay 与 `/tmp` 空间
- 插件市场预演：索引拉取、依赖求解、下载与 SHA256、上传复核、`apk add --simulate`
- 真实安装 `luci-app-uhttpd`：安装成功，回读版本正确（包数 290 → 291）
- 卸载回滚：`apk del` 成功，回读包数恢复 290，远端临时目录已清理

尚未验证：

- 其它机型、CPU 架构与固件版本
- opkg 固件的真实安装与卸载
- 密码、私钥、SSH Agent 三种认证在实机上的连通性（实机使用 none）
- 控制中心里系统 / 无线 / DHCP / UCI 等配置写入路径的实机执行
- 界面逐屏人工点击回归（界面通过 `?mock=1` 与后端集成测试验证）
- SQM 等依赖 kmod 的插件（受硬性限制，不会安装）

## 安全说明

- 密码不写入磁盘；日志不记录密码或私钥。
- 主机密钥采用 TOFU 固定，首次确认后写入配置，不匹配即中止连接。
- 所有设备写入先经过参数校验与授权检查，失败会给出具体原因。
- 仓库不包含任何凭证、设备授权文件、运行日志或本地缓存。

## 许可证

MIT，见 [LICENSE](LICENSE)。
