# router-prolite 插件安装链路设计方案

- 版本：v0.2（离线安装修订稿，未改任何 router-prolite 代码）
- 对象：`router-prolite`（Go + Wails v2，SSH 执行型 OpenWrt 桌面助手）
- 目标设备：MelonPi M21 / MT7621 / mipsel_24kc / ImmortalWrt master（6.18.x，squashfs + overlay ≈2.1MB）
- 唯一安装路径：**桌面端下载 → 桌面端校验 → 上传设备 → 离线安装**。设备端不执行 `apk update` / `opkg update`，不在线拉包。

---

## 0. 结论

1. 现状**不具备**任何安装能力：只有 SSH exec（`sess.CombinedOutput`）、UCI 写入、init 脚本启停；无 SFTP、无上传、无 `opkg install` / `apk add`。
2. 本方案新增一条独立链路：`只读采集设备源配置 → 桌面拉索引 → 依赖求解 → 并发下载 → 本地校验 → 上传 /tmp → 远端校验 → 离线批量安装 → 回读 →（失败）回滚`。
3. **设备端全程离线**：安装命令只使用已上传到 `/tmp` 的本地包文件；`apk update` / `opkg update` 被明确移除，设备不访问任何 feed。
4. 传输不直接依赖 SFTP：OpenWrt 常见镜像**没有 `openssh-sftp-server`**，SFTP 子系统会握手失败。方案采用「SFTP 优先 + `cat > file` 流式回退」双通道。
5. 包管理器以设备实测为准：项目 README 记录 MelonPi M21 为 **apk**，需求描述为 opkg。设计必须同时支持，连接时探测，不能写死。
6. 在 MelonPi M21 的 2.1MB overlay 上，绝大多数 LuCI 插件会在**空间预检**阶段被拒绝；这是设备约束，不是程序缺陷。方案要求明确提示，而不是静默失败。

### 为什么必须走“桌面下载 + 离线安装”

| 理由 | 说明 |
| --- | --- |
| 设备网络不稳定 | 设备侧 `apk/opkg` 在线安装需要设备自己访问镜像站，失败点不可控；桌面端网络、代理、重试策略都可控 |
| ABI 版本可控 | 索引、包版本、内核 ABI 在桌面端一次性锁定，避免“设备上装到一半源变了” |
| 可预检 | 下载前即可判定架构、内核、依赖闭包、overlay 空间，失败发生在无副作用阶段 |
| 可审计 | 桌面端保留完整包清单、URL、SHA256、下载时间，便于复现 |
| 不依赖设备端工具链 | 设备只需 `apk`/`opkg` 二进制与本地文件，不需要 `wget`、CA 证书、DNS |

---

## 1. 目标与非目标

### 1.1 目标

| 编号 | 目标 |
| --- | --- |
| G1 | 安装用户指定的包：既支持**本地已有包文件**，也支持**桌面端从设备配置的官方 feed 下载**后安装 |
| G2 | 依赖只从**设备当前配置的 feed**（`/etc/apk/repositories`、`/etc/opkg/*.conf`）解析，且全部由桌面端下载；不内置商店 |
| G3 | 下载、解析、校验、上传、安装、回读、回滚全流程有明确阶段与可读错误 |
| G4 | 提供两级 dry-run：桌面端依赖求解预演 + 设备端离线 simulate，不落盘、不改包数据库 |
| G5 | 全流程可取消、可观测（阶段进度 + 命令日志 + 每文件进度） |
| G6 | 保持现有只读采集、UCI 编辑、服务启停逻辑**零行为变更** |
| G7 | 设备端零外网依赖：安装阶段只用 `/tmp` 内的本地文件 |

### 1.2 非目标

- **不做设备端在线安装**：不执行 `apk update` / `opkg update`，不让设备自己下载包（唯一例外：设备上残留的历史缓存文件不参与本流程）。
- 不做在线插件商店：不内置插件目录、不展示 feed 全量包列表、不做“一键安装推荐插件”。
- 不做任意 URL 下载：下载源只能是设备上已配置的 feed URL；用户手填 URL 属于未决项（§12），默认关闭。
- 不自动升级已安装包（只允许“安装”和“明确版本替换”，升级作为独立功能另议）。
- 不接管 LuCI 的包管理页面，不修改设备上的 feed 配置、不写 `/etc/apk/repositories`、不写 `/etc/opkg/*.conf`。
- 不改 `console.go` 的采集脚本语义；只允许**追加**探测字段（包管理器、架构、内核）。

### 1.3 与现有代码的边界

| 现有文件 | 处理方式 |
| --- | --- |
| `console.go`（pluginCatalog / detectPlugins / parsePackages） | 不动。安装功能不复用“插件目录”概念 |
| `router.go` 的 `execWithClient` | 不动。新增独立的长任务执行函数 |
| `app.go` 的 `ExecCommand` | 不动（25s / 2000 字符限制保留给终端） |
| `meta.go` 的 `metaScript` | 仅追加 `@@PKGMGR` 等探测行，解析保持向后兼容 |
| `frontend/app.js` | 新增安装面板；软件包页只加一个入口按钮 |

---

## 2. 现状约束（设计依据）

| 事实 | 代码位置 | 对设计的影响 |
| --- | --- | --- |
| 仅 SSH 命令通道，无 Stdin 写入 | `router.go:185-219` | 必须新增流式上传通道 |
| `CombinedOutput` 一次性返回，25s 超时 | `router.go:200`、`app.go:262` | 下载/安装是长任务，必须独立通道与超时 |
| UCI 写入通过 `ubus call uci` | `actions.go:50-88` | 回滚 UCI 走同一套接口，无需新机制 |
| 已装包列表靠 `apk info -v` / `opkg list-installed` | `console.go:27` | 包管理器探测、已装版本比对复用该输出格式 |
| 服务白名单机制 | `actions.go:462-518` | 安装后服务启停应复用白名单 + 二次确认 |
| TOFU 主机指纹 | `ssh.go:134-166` | 安装任务必须绑定当前 `gen`，重连即中止 |
| 无任何包签名/哈希校验 | 全仓库 | 校验需从零新增（索引签名 + 包 SHA256 + 包签名） |
| 无 HTTP 客户端 | `go.mod` 无相关依赖 | 桌面下载需新增 `net/http`（标准库，无需第三方） |

---

## 3. 传输通道（桌面 → 设备）

### 3.1 依赖与新增位置

新增依赖：`github.com/pkg/sftp v1.13.x`（纯 Go，复用现有 `*ssh.Client`，不引入新连接）。

新增文件 `transfer.go`，不改 `ssh.go` 的认证逻辑：

```go
// transfer.go
type TransferProgress struct {
    Index int   `json:"index"` // 第几个文件
    Count int   `json:"count"`
    Name  string `json:"name"`
    Sent  int64 `json:"sent"`
    Total int64 `json:"total"`
    Bps   int64 `json:"bps"`
}

// 复用当前 SSH 连接；返回 gen 用于检测期间是否发生重连
func (r *Router) sftpClient() (client *sftp.Client, gen uint64, err error)
```

取连接的方式与 `execWithClient` 一致：在 `r.mu` 下读 `r.client` 与 `r.gen`，`sftp.NewClient(client)`。`ssh.Client` 支持多 channel，SFTP 与 exec 可并存；任务结束 `defer c.Close()`。

### 3.2 SFTP 可用性探测与回退（关键）

OpenWrt 默认 dropbear 不自带 sftp-server，是否有 SFTP 取决于 `openssh-sftp-server` 包：

```sh
for p in /usr/libexec/sftp-server /usr/lib/sftp-server /usr/lib/ssh/sftp-server; do
  [ -x "$p" ] && echo "SFTP_OK $p" && break
done
```

- 探测通过 → 走 SFTP。
- 探测失败 / `sftp.NewClient` 返回 `subsystem request failed` → 回退到 exec 流式写：

```sh
# 由 Go 侧 StdinPipe 写入二进制，命令侧不做编码
cat > /tmp/prolite-<job>/pkgs/<name>.part && \
mv /tmp/prolite-<job>/pkgs/<name>.part /tmp/prolite-<job>/pkgs/<name>
```

回退通道需要新增 `execStreamStdin`（现有 `execWithClient` 无 stdin）：

```go
func (r *Router) execStreamStdin(ctx context.Context, cmd string,
    src io.Reader, onProgress func(int64)) (string, error)
```

两条通道上传完成后都必须执行**远端大小 + SHA256 双重校验**，任一不符即删除临时文件并报 `HASH_MISMATCH`。不要用 `base64`（体积 +33%，且无必要）。

### 3.3 分块与进度

| 参数 | 取值 | 理由 |
| --- | --- | --- |
| 块大小 | 32 KiB | pkg/sftp 单包上限 32KB 附近，避免额外分包 |
| 进度回调节流 | 200ms | 与 Wails 事件频率匹配，避免刷爆 WebView |
| 单块写超时 | 15s | 区分“慢”与“死” |
| 整体重试 | 3 次，500ms/1s/2s 退避 | 覆盖 WiFi 抖动 |
| 单文件上限 | 32MB | `/tmp` 是 tmpfs（内存盘），241MB 设备上大文件会触发 OOM |
| 批量总量上限 | 64MB（可配置） | 依赖闭包可能一次十几个包，必须整体限额 |

实现要点：

```go
// 伪代码
rf, _ := c.OpenFile(part, os.O_WRONLY|os.O_CREATE)
var off int64
if st, err := c.Stat(part); err == nil { off = st.Size() } // 断点续传
lf.Seek(off, io.SeekStart)
buf := make([]byte, 32*1024)
for {
    n, err := lf.Read(buf)
    if n > 0 {
        if _, werr := rf.WriteAt(buf[:n], off); werr != nil { /* 重试当前块 */ }
        off += int64(n); progress(off, total)
    }
    if err == io.EOF { break }
}
```

- 若当前 `pkg/sftp` 版本的 `File.WriteAt` 不可用，退化为“整文件重传”，不做续传。
- 只写 `.part`，完成后 `mv` 为正式文件名，避免半文件被误装。
- 多文件按**依赖拓扑序**逐个上传；任一失败即整体中止并清理本次任务目录。

### 3.4 目标路径与清理

```
/tmp/prolite-<jobID>/
  ├── pkgs/
  │   ├── libfoo_1.2-1_mipsel_24kc.ipk
  │   ├── kmod-xxx_....ipk
  │   └── target_....ipk
  ├── upload.part/            # 上传中间态（逐文件 .part）
  ├── before.pkgs
  ├── before.init
  ├── before.df
  ├── before.cfg.sha256
  └── backup-etc-config.tar.gz  # 回滚备份（可选）
```

- 优先 `/tmp`（tmpfs），**绝不**先写 `/overlay`。
- 上传前检查 `/tmp` 可用空间 ≥ 全部包体 ×1.15；不足则报 `SPACE_TMP`。
- 每个任务 `defer` 清理；进程异常退出时，下次连接启动执行一次清扫：

```sh
rm -rf /tmp/prolite-* 2>/dev/null
```

- 该清扫只在用户明确触发安装或连接建立时执行，不影响其他 `/tmp` 内容。
- 安装失败时保留目录用于导出日志；用户点击“关闭”后再删除。

### 3.5 断线重传

| 场景 | 策略 |
| --- | --- |
| 上传中掉线 | 重连后 `Stat` 远端 `.part`，从已有偏移继续；最多 3 次 |
| 重连后 `gen` 变化 | 取消当前任务，返回 `NET_DISCONNECTED`，由用户重试 |
| 安装命令执行中掉线 | **不自动重试**（包管理器可能已部分落盘），进入“待确认”状态，提示用户重新连接后执行回滚/校验 |
| 校验和失败 | 删除远端文件，整包重传一次；再失败即终止 |
| 设备重启 | 任务标记为 `ABORTED`，不做自动续装 |

---

## 4. 包格式与包管理器判定

### 4.1 探测（连接时一次性完成，随 Meta 缓存）

在 `meta.go` 的 `metaScript` 追加（不改动已有段）：

```sh
echo '@@PKGMGR'; command -v apk 2>/dev/null; command -v opkg 2>/dev/null
echo '@@PKGVER'; apk --version 2>/dev/null; opkg --version 2>/dev/null
echo '@@ARCH'; apk --print-arch 2>/dev/null; cat /etc/apk/arch 2>/dev/null; opkg print-architecture 2>/dev/null
echo '@@KERNEL'; uname -r
echo '@@KPKG'; apk info kernel 2>/dev/null; opkg info kernel 2>/dev/null
echo '@@PKGDBS'; ls -l /lib/apk/db/installed /usr/lib/opkg/status 2>/dev/null
echo '@@OVERLAY'; df -k /overlay /tmp / 2>/dev/null
echo '@@FEEDS_APK'; cat /etc/apk/repositories 2>/dev/null
echo '@@FEEDS_OPKG'; cat /etc/opkg/distfeeds.conf /etc/opkg/customfeeds.conf 2>/dev/null
echo '@@KEYS'; ls /etc/apk/keys /etc/opkg/keys 2>/dev/null
```

`Meta` 新增字段：

```go
type Meta struct {
    // ...既有字段
    PkgMgr     string   // "apk" | "opkg" | ""
    PkgArch    string   // 归一化后的主架构，如 mipsel_24kc
    ArchAll    []string
    Kernel     string   // uname -r
    KernelPkg  string   // 内核包精确版本（含 ABI hash）
    FeedURLs   []Feed   // 只读解析结果
    APKKeys    []string
}
```

### 4.2 判定顺序（两者共存时）

| 顺序 | 规则 | 结果 |
| --- | --- | --- |
| 1 | 只有 `apk` | apk |
| 2 | 只有 `opkg` | opkg |
| 3 | 两者都有 | 比较 `/lib/apk/db/installed` 与 `/usr/lib/opkg/status` 的**包条目数**，多者为准；相同则优先 apk |
| 4 | 都没有 | `PKG_MGR_UNKNOWN`，安装入口禁用（采集功能不受影响） |

判断理由：ImmortalWrt 24.10 之前是 opkg，master/25.x 起切 apk；过渡固件可能残留另一个二进制，不能只看 `command -v`。

### 4.3 两种包管理器的差异（命令分支依据）

| 维度 | opkg | apk |
| --- | --- | --- |
| 包文件 | `.ipk`，`ar` 归档：`debian-binary` + `control.tar.gz` + `data.tar.gz` | `.apk`，gzip tar：`.PKGINFO` + 数据段（v2/v3 结构不同） |
| 数据库 | `/usr/lib/opkg/status`、`/usr/lib/opkg/info/` | `/lib/apk/db/installed`、`/etc/apk/world` |
| 源配置 | `/etc/opkg/distfeeds.conf`、`customfeeds.conf` | `/etc/apk/repositories` |
| 信任根 | `/etc/opkg/keys`（usign/ed25519） | `/etc/apk/keys`（RSA PEM） |
| 索引文件 | `Packages.gz` + `Packages.sig` | `APKINDEX.tar.gz`（内含签名成员） |
| 已装列表 | `opkg list-installed`（`name - version`） | `apk info -v`（`name-version`，项目已按此解析） |
| 模拟安装 | `opkg install --noaction <files...>` | `apk add --simulate <files...>` |
| 本地安装 | `opkg install <files...>` | `apk add <files...>` |
| 卸载 | `opkg remove <name>` | `apk del <name>` |
| 架构查询 | `opkg print-architecture`（多行 `arch <name> <prio>`） | `apk --print-arch` / `/etc/apk/arch` |
| 版本比较 | `opkg compare-versions <a> <b>` | `apk version -t <a> <b>`（旧版）；否则自实现比较器 |
| 索引刷新 | **设备端禁止**；桌面端拉 `Packages.gz` | **设备端禁止**；桌面端拉 `APKINDEX.tar.gz` |

apk 3.x 的 CLI 有变化，探测到版本后分支：

- `apk info -v` 不可用 → 回退 `apk list --installed`；
- `apk version -t` 不可用 → 使用内置 `compareVersion`（`rN` 后缀、epoch、`~` 语义）；
- `--no-network` 不可用 → 使用 `--repositories-file /dev/null`（或空文件）阻断网络。

### 4.4 支持的输入格式

| 扩展名 / 魔数 | 处理 |
| --- | --- |
| `.ipk` 且首 8 字节 `!<arch>\n` | 支持（需 opkg 设备） |
| `.apk` 且 gzip/tar 且内含 `.PKGINFO` | 支持（需 apk 设备） |
| `.tar.gz` / `.tgz` | **默认拒绝**（`FORMAT_UNSUPPORTED`）。它可能是源码包，也可能是 apk v3 容器，无法可靠区分安装语义 |
| 裸二进制 / `.sh` | **拒绝**。安装脚本绕过包数据库，无法回滚、无法校验依赖 |
| 扩展名与魔数不一致 | 拒绝，防止把 ipk 改名成 apk 误导用户 |
| 下载得到的 feed 包 | 由索引元数据判定格式；落盘后仍做同样的魔数复核 |

本地解析在桌面完成（不需要设备装 `ar`/`tar`）：

```go
// pkgmeta.go
func ParseIPK(path string) (*PkgMeta, error)   // 自实现 ar 读取（约 60 行）
func ParseAPK(path string) (*PkgMeta, error)   // archive/tar + compress/gzip
func DetectPackage(path string) (PkgFormat, error)
```

```go
type PkgMeta struct {
    Format        string   // "ipk" | "apk"
    Name          string
    Version       string
    Arch          string
    InstalledSize int64    // ipk: control Installed-Size；apk: .PKGINFO size
    Depends       []string
    Provides      []string
    Conflicts     []string
    Replaces      []string
    HasScripts    bool     // preinst/postinst/preun/postun
    LocalSHA256   string
}
```

若 `Installed-Size` 缺失：流式解压 `data.tar.gz` 统计文件大小（默认上限 64MB，超过即放弃精确估算并标记 `sizeEstimated=true`）。

---

## 5. 桌面端下载与依赖解析（离线链路核心）

### 5.1 从设备只读读取源配置

**只允许读，不允许写**。实现上只调用 `cat` / `ls` / `test`，并把整段命令标记为 `readonly`（审计日志记录“未写入”）。

```sh
# apk
cat /etc/apk/repositories
cat /etc/apk/arch 2>/dev/null
ls /etc/apk/keys 2>/dev/null
apk info kernel 2>/dev/null
cat /lib/apk/db/installed 2>/dev/null | head -c 2097152   # 已装库，2MB 上限

# opkg
cat /etc/opkg/distfeeds.conf 2>/dev/null
cat /etc/opkg/customfeeds.conf 2>/dev/null
ls /etc/opkg/keys 2>/dev/null
opkg info kernel 2>/dev/null
cat /usr/lib/opkg/status 2>/dev/null | head -c 4194304     # 已装库，4MB 上限

# 两者都要
cat /etc/openwrt_release 2>/dev/null
uname -r
df -k /overlay /tmp /
```

解析规则：

| 输入 | 规则 |
| --- | --- |
| apk 行 `https://host/path` | 直接作为 feed base URL |
| apk 行 `@tag https://host/path` | `tag` 作为源优先级分组，URL 仍可下载 |
| apk 行 `#` 开头 / 空行 | 跳过 |
| opkg 行 `src/gz <name> <url>` | `name` 作为源标识，`url` 为 base |
| opkg 行 `src <name> <url>` | 同上，表示未压缩 `Packages`（仍优先尝试 `Packages.gz`） |
| URL 含 `$ARCH` / `${ARCH}` / `%ARCH%` | 用设备架构替换（不同发行版写法不一） |
| `file://` / 相对路径 | 跳过并在报告中提示“本地源不支持桌面下载” |
| `http://` | 允许但警告（降级提示），默认仍尝试 HTTPS 优先替换 |

同时解析出：

- `arch`：`apk --print-arch`，或 `opkg print-architecture` 的第一主架构；
- `kernel`：`uname -r`；
- `kernelPkg`：`apk info kernel` / `opkg info kernel` 的精确版本（含 `~<ABI hash>`），这是判断 kmod 的权威依据；
- 已装包集合：从 `installed` / `status` 中解析 `name`、`version`、`arch`、`provides`。

**漂移检测**：把 `/etc/openwrt_release` 的 `DISTRIB_ARCH`、`DISTRIB_REVISION`、feed 路径中的 `targets/<target>/<subtarget>` 与设备 `board_name` 对比；不一致时给出 `IDX_STALE` 警告（不阻断，但默认要求用户确认）。

### 5.2 桌面端拉取索引

#### apk：`APKINDEX.tar.gz`

- URL：`<feedBase>/APKINDEX.tar.gz`。
- 结构：gzip tar，包含 `APKINDEX` 文本成员与签名成员。
- v2/v3 差异：
  - v2 索引记录以空行分隔，字段前缀 `C: P: V: A: S: I: T: U: L: o: m: t: c: D: p: i: k: r:`；
  - v3 索引仍为 tar.gz，但字段集合与包体格式扩展；解析器按“已知字段取值、未知字段忽略”实现，保证向前兼容；
  - 签名成员名可能是 `.SIGN.RSA.<keyname>`（SHA1）或 `.SIGN.RSA256.<keyname>`（SHA256）。
- 验签：
  1. 从设备只读获取 `/etc/apk/keys/*`（PEM 公钥）；
  2. 用签名成员名前缀确定算法；
  3. 对 `APKINDEX` 成员的**原始字节**做 RSA 校验；
  4. 找不到匹配公钥 → `IDX_SIGNATURE`（默认阻断，可显式选择“未验证索引”继续）。

#### opkg：`Packages.gz` / `Packages` + `Packages.sig`

- URL：`<feedBase>/Packages.gz`，失败回退 `<feedBase>/Packages`。
- `Packages.sig` 为 usign（ed25519）分离签名；验签对象是**未压缩的 `Packages` 内容**。
- 验签步骤：
  1. 读取 `/etc/opkg/keys/<keyid>` 公钥；
  2. 解析 usign 签名容器（含头部字节），用 ed25519 校验；
  3. 失败 → `IDX_SIGNATURE`。
- 实现风险：usign 是 OpenWrt 自有封装，Go 侧需按其格式解析；M1 若无法在合理成本内完成，允许“HTTPS + 索引 SHA256 记录 + 包级签名留到设备安装时验证”的过渡模式，但必须在 UI 明示“索引未验证”。

#### 索引条目解析

- opkg 每条 stanza 取：`Package`、`Version`、`Architecture`、`Depends`、`Provides`、`Conflicts`、`Replaces`、`Installed-Size`、`Filename`、`Size`、`SHA256sum`。
- apk 每条记录取：`P`（名）、`V`（版本）、`A`（架构）、`S`（包大小）、`I`（安装后大小）、`D`（依赖）、`p`（provides）、`r`（replaces，按实际索引字段为准）、`C`（校验和，带算法前缀）。
- 包的下载 URL：
  - opkg：`<feedBase>/<Filename>`；
  - apk：按约定 `<feedBase>/<P>-<V>.apk`（若索引提供显式文件名则优先使用）。

### 5.3 缓存与过期

目录：`os.UserConfigDir()/ProLite/cache/`

```
cache/
  feeds/<host>/<sha1(feedURL)>/APKINDEX.tar.gz | Packages.gz | Packages
  feeds/<host>/<sha1(feedURL)>/meta.json      # etag/last-modified/fetchedAt/签名结果
  packages/<sha256前2位>/<sha256>.<ext>        # 已下载包，按内容寻址
```

| 策略 | 取值 |
| --- | --- |
| 索引 TTL | snapshot：6h；release：24h |
| 条件请求 | `If-None-Match`（ETag）、`If-Modified-Since`；304 直接复用 |
| 手动刷新 | 用户点击“刷新索引”绕过 TTL |
| 包缓存 | 按 SHA256 命名，命中即复用，不重复下载 |
| 容量上限 | 512MB，LRU 淘汰；启动时清理 |
| 失效条件 | 设备 feed URL 变化、架构变化、`meta.json` 中签名为失败、用户手动清除 |
| 并发 | 同一 URL 只允许一个在途请求（singleflight） |

### 5.4 依赖闭包求解

输入：目标包（本地文件或 feed 包名 + 版本约束）、设备已装包集合、全部 feed 索引。

步骤：

1. **构建候选宇宙**
   - 所有 feed 条目 + 已装包（已装包视为“最高优先级可用提供者”，避免重复下载 base 依赖）。
   - 建立 `name → []candidate` 与 `provides → []candidate` 两个索引。
   - 过滤架构：`arch == deviceArch || arch in {"all","noarch"}`。
2. **解析依赖表达式**
   - opkg：`a, b (>= 1.2), c (<< 2.0), d | e`；
   - apk：`a>=1.2 b=1.2 c~1.2 so:libx.so.1 cmd:foo !conflict`；
   - 统一成 `{name, op, version, alternativeGroup, negative}`。
3. **递归求解**（DFS + 已选集合 + 回代）
   - 对每个依赖：
     1. 若已装且满足约束 → 标记 satisfied，不再下载；
     2. 若已选且满足 → 跳过；
     3. 否则在候选中选择：优先同名真实包，其次 `Provides` 提供者；版本选满足约束的最高版本；同版本按 feed 顺序（设备配置顺序即优先级）；
     4. 记录选择原因（便于报告）。
   - 备选项（`a | b`）按顺序尝试，全部失败才报错。
4. **虚拟包 / ABI 依赖**
   - `so:`、`cmd:`、`lib:`、`abi:` 前缀依赖：在 `provides` 中匹配；匹配不到时视为不可解（`DEP_UNRESOLVED`）。
   - apk 的 `!pkg` 负依赖：加入冲突集合，选中集合与其相交即失败。
5. **kmod 与内核 ABI**
   - 若包名以 `kmod-` 开头，或其 `Depends` 含 `kernel`：
     - 取依赖中的精确版本（通常 `kernel (=6.18.26~<hash>)`）；
     - 与设备 `kernelPkg` 的 `~<hash>` 严格比对；
     - 不等 → `KERNEL_MISMATCH`，报告“设备内核 ABI 与 feed 不一致，需使用与固件同源的 kmod”。
   - kmod 默认不允许用户跳过此检查（M1/M2）。
6. **冲突检测**
   - `Conflicts` 两两相交 → `VERSION_CONFLICT`；
   - `Replaces` 视为“替换”，需明确展示“将替换已装包 X”；
   - 目标包与已装同版本 → 拒绝（提示已安装）；
   - 目标版本低于已装版本 → 默认拒绝（降级需显式开关）。
7. **拓扑排序**
   - 依赖 → 被依赖建边，Kahn 算法输出安装顺序（依赖在前）；
   - 有环 → `DEP_UNRESOLVED`，报告环路径；
   - 同一包被多个父依赖要求不同版本 → 取满足所有约束的最高版本；无交集 → `VERSION_CONFLICT`。
8. **产出 `InstallPlan`**

```go
type InstallPlan struct {
    JobID       string
    Packages    []PlanItem   // 已按拓扑序排列
    TotalSize   int64        // 压缩包总大小
    TotalInstall int64       // 安装后总大小（overlay 估算）
    Warnings    []PlanWarning
}

type PlanItem struct {
    Name, Version, Arch string
    URL                 string
    Filename            string
    Size                int64
    InstalledSize       int64
    SHA256              string   // 索引提供则用，否则下载后回填
    AlreadyInstalled    bool
    Reason              string   // 为什么被选中（依赖路径）
}
```

### 5.5 下载与校验

| 项 | 设计 |
| --- | --- |
| 并发 | 全局 4，单 host 2；包总量 <4 时串行 |
| 断点续传 | HTTP `Range: bytes=<n>-`，落盘 `.part`，校验前 `mv` |
| 重试 | 每文件 3 次，退避 1s/2s/4s；404 不重试 |
| 超时 | 单文件 300s；整批 20min；可取消 |
| 校验 | 文件大小 + 索引校验和；opkg 用 `SHA256sum`；apk 用 `C:`（按算法前缀，无法识别时回退 SHA256 自算并与缓存记录比对） |
| 签名 | 桌面端尽力验证包签名（apk RSA / opkg usign）；无法验证时标记 `signaturePending`，交由设备安装阶段裁决 |
| 磁盘 | 下载前检查缓存盘可用 ≥ 总量 ×2；不足报 `DL_DISK` |
| 总量预算 | 默认 256MB；超过报 `DL_TOO_LARGE`（可配置） |
| 失败 | 删除该文件 `.part`，其余文件继续；汇总后统一报错 |
| 目录穿越 | 只使用索引文件名，禁止从包里取路径写盘 |

下载完成后每个包再做一次**文件级校验**（见 §6.2）。

### 5.6 明确不支持的情况

| 情况 | 处理 |
| --- | --- |
| feed 需要设备侧凭据 / 内网 VPN 才能访问 | 桌面端不可达 → `IDX_UNREACHABLE`，提示改用本地包 |
| 包的内容或依赖需由设备脚本动态生成（如安装时调用 `ubus`、读 `/etc/board.json` 决定依赖） | 依赖求解无法覆盖；标记 `preflightIncomplete`，要求用户确认后继续，或拒绝 |
| apk `install_if` 由设备本地包集合触发 | 只在已装库完整时求解；否则跳过可选包并提示 |
| 设备上自编译、不在任何 feed 中的包 | 索引中找不到 → `PKG_NOT_FOUND`，只能走“本地文件”模式 |
| 需要 `--force-*` 才能安装的包 | 默认拒绝 |
| 只有 `file://` 本地源 | 桌面无法访问，跳过并提示 |
| 包体 >32MB 或依赖闭包 >64MB（上传）/256MB（下载） | 拒绝，提示手动处理 |

---

## 6. 预检（三段式）

### 6.1 阶段划分

| 阶段 | 时机 | 数据来源 | 拦截内容 | 副作用 |
| --- | --- | --- | --- | --- |
| **A. 元数据预检（下载前）** | 拉完索引、下载前 | 索引 + 设备只读信息 | 包管理器/格式、架构、内核 ABI、依赖闭包、冲突、总大小、overlay 空间 | 无 |
| **B. 文件级校验（下载后）** | 下载完成、上传前 | 本地文件 + 索引记录 | 魔数/格式、内部元数据与索引一致、SHA256、包签名、大小 | 仅本地缓存 |
| **C. 设备级校验（上传后）** | 上传完成、安装前 | 设备 `/tmp` 文件 + 已装库 | 远端 SHA256、离线 simulate、包数据库冲突复核 | `/tmp` 临时文件 |

### 6.2 A 段：下载前元数据预检

| # | 检查 | 数据来源 | 失败错误码 | 阻断 |
| --- | --- | --- | --- | --- |
| A1 | 包管理器与目标包格式匹配 | `Meta.PkgMgr` + 索引/本地包 | `PKG_MGR_MISMATCH` | 是 |
| A2 | 包存在且版本可解析 | 索引 / 本地解析 | `PKG_NOT_FOUND` | 是 |
| A3 | 架构匹配 | `PkgMeta.Arch` vs `Meta.PkgArch` | `ARCH_MISMATCH` | 是 |
| A4 | kmod 内核 ABI 匹配 | `Depends` 中 `kernel=...` vs `Meta.KernelPkg` | `KERNEL_MISMATCH` | 是 |
| A5 | 依赖闭包可解 | solver | `DEP_UNRESOLVED` | 是 |
| A6 | 冲突/替换/降级 | solver | `VERSION_CONFLICT` | 是 |
| A7 | overlay 空间够（含解包后） | `Installed-Size ×1.15` vs `df -k /overlay` | `SPACE_OVERLAY` | 是 |
| A8 | `/tmp` 空间够（上传总量） | 包体 ×1.15 vs `df -k /tmp` | `SPACE_TMP` | 是 |
| A9 | 索引与设备版本漂移 | 已装库 vs 索引 | `IDX_STALE` | 警告 |
| A10 | 危险包类型 | 包名/依赖 | `PKG_DANGEROUS` | 是 |
| A11 | 下载总量在预算内 | `Sum(Size)` | `DL_TOO_LARGE` | 是 |

A 段结束后产出 `InstallPlan` 与可读报告；用户可在此选择 dry-run 直接结束。

### 6.3 B 段：下载后文件级校验

| # | 检查 | 失败错误码 |
| --- | --- | --- |
| B1 | 文件非空、大小与索引 `Size`/`S` 一致 | `DL_HASH` |
| B2 | 魔数与格式正确（ipk=ar，apk=tar.gz） | `FORMAT_UNSUPPORTED` |
| B3 | 包内 `P/V/A` 与索引记录一致 | `DL_HASH` |
| B4 | SHA256 与索引一致（无索引值则记录实测值） | `DL_HASH` |
| B5 | 包签名（apk RSA / opkg usign）可验证（可降级为待设备验证） | `SIGNATURE_INVALID` |
| B6 | 包内脚本类型统计（preinst/postinst）并展示 | 警告 |

### 6.4 C 段：上传后设备级校验

| # | 检查 | 命令 | 失败错误码 |
| --- | --- | --- | --- |
| C1 | 远端文件大小一致 | `stat -c %s` / `wc -c` | `HASH_MISMATCH` |
| C2 | 远端 SHA256 一致 | `sha256sum` | `HASH_MISMATCH` |
| C3 | 离线 simulate（不联网） | `opkg install --noaction` / `apk add --simulate --no-network` | `DEP_UNRESOLVED` / `INSTALL_FAILED` |
| C4 | 包数据库复核 | `list-installed` / `apk info -v` | `VERSION_CONFLICT` |
| C5 | 目录可写、剩余空间 | `df -k /overlay /tmp` | `SPACE_OVERLAY` / `SPACE_TMP` |

C3 的离线方式：

```sh
# apk：优先禁网
apk add --simulate --no-network /tmp/prolite-<job>/pkgs/*.apk
# 不支持 --no-network 时
apk --repositories-file /dev/null add --simulate /tmp/prolite-<job>/pkgs/*.apk

# opkg：不给它联网机会，依赖已全部在本地
opkg install --noaction /tmp/prolite-<job>/pkgs/*.ipk
```

---

## 7. 离线安装执行与长任务

### 7.1 分阶段

| 阶段 | 动作 | 超时 | 可取消 |
| --- | --- | --- | --- |
| S0 prepare | 建任务目录、快照已装/init/config/df、备份 `/etc/config` | 30s | 是 |
| S1 upload | 按拓扑序上传全部包，逐文件进度 | 单文件按体积，单块 15s | 是 |
| S2 verify | 远端大小 + `sha256sum` | 30s | 是 |
| S3 simulate | 离线 simulate（C3） | 180s | 是 |
| S4 install | 离线批量安装（依赖在前） | 900s | 是（有风险） |
| S5 verify | 回读包版本 / init / config 差异 | 30s | 是 |
| S6 activate | 可选：`enable` + `start` 新服务 | 30s | 否（短） |
| S7 cleanup | 删除 `/tmp/prolite-<job>` | 10s | 否 |

### 7.2 长任务通道（绕开 25s / 2000 字符）

现有 `ExecCommand` 的限制不能改（终端语义）。新增：

```go
// router.go 追加，不改 execWithClient
func (r *Router) execJob(ctx context.Context, cmd string,
    timeout time.Duration, onLine func(string)) (JobOutput, error)

type JobOutput struct {
    ExitCode int
    Output   string
    TimedOut bool
}
```

实现要点：

- `sess.Start(cmd)` + `StdoutPipe` / `StderrPipe`，逐行回调 `onLine`（前端日志）。
- `sess.Wait()` 放 goroutine，`ctx.Done()` 时先 `sess.Signal(ssh.SIGINT)`，5s 后 `sess.Close()`。
- 输出上限沿用 `maxOutput`（192KB），日志只保留尾部 4KB 回传前端。
- 任务互斥：包管理器有全局锁，`Router` 内增加 `installMu sync.Mutex`，同一时间只允许一个安装任务。
- 设备端命令显式设置 `http_proxy=` 等无效值不是必须；关键是**不调用** `update`，且 simulate/install 都针对本地文件。

### 7.3 离线安装命令序列

上传目录：`/tmp/prolite-<job>/pkgs/`；顺序：**依赖在前、被依赖在后**（桌面 solver 已排好）。

#### apk

```sh
# 0) 准备与快照
mkdir -p /tmp/prolite-<job>/pkgs
apk info -v > /tmp/prolite-<job>/before.pkgs
ls /etc/init.d > /tmp/prolite-<job>/before.init
df -k /overlay /tmp > /tmp/prolite-<job>/before.df
tar -czf /tmp/prolite-<job>/backup-etc-config.tar.gz -C / etc/config

# 1) 上传后校验
sha256sum /tmp/prolite-<job>/pkgs/*.apk

# 2) 离线 simulate（不联网）
apk add --simulate --no-network /tmp/prolite-<job>/pkgs/*.apk
# 若版本不支持 --no-network：
apk --repositories-file /dev/null add --simulate /tmp/prolite-<job>/pkgs/*.apk

# 3) 离线安装（一次性传入全部包，保证依赖可解析）
apk add --no-network /tmp/prolite-<job>/pkgs/*.apk
# 或按拓扑序逐个安装（日志更清晰）：
# for f in libfoo-1.0-r1.apk kmod-xxx-....apk target-....apk; do
#   apk add --no-network "/tmp/prolite-<job>/pkgs/$f" || exit 1
# done

# 4) 回读
apk info -v | grep -E '^<name>-'
```

`--allow-untrusted` 使用边界：

- **默认绝不使用**。设备 `/etc/apk/keys` 已包含官方 feed 公钥，官方包应能正常验签。
- 仅当用户显式勾选“允许未签名/第三方包”，且该包被标记为不受信任时，单独对**该文件**追加：
  ```sh
  apk add --allow-untrusted --no-network /tmp/prolite-<job>/pkgs/<only-that-file>.apk
  ```
- 混合批次必须拆分：先装有签名的，再单独装未签名的；**禁止**对整个 glob 加 `--allow-untrusted`。
- 每次使用都写审计日志（包名、SHA256、用户确认时间）。

#### opkg

```sh
# 0) 准备与快照
mkdir -p /tmp/prolite-<job>/pkgs
opkg list-installed > /tmp/prolite-<job>/before.pkgs
ls /etc/init.d > /tmp/prolite-<job>/before.init
df -k /overlay /tmp > /tmp/prolite-<job>/before.df
tar -czf /tmp/prolite-<job>/backup-etc-config.tar.gz -C / etc/config

# 1) 校验
sha256sum /tmp/prolite-<job>/pkgs/*.ipk

# 2) 离线 simulate
opkg install --noaction /tmp/prolite-<job>/pkgs/*.ipk

# 3) 离线安装：按拓扑序，依赖在前
opkg install \
  /tmp/prolite-<job>/pkgs/libfoo_1.0-1_mipsel_24kc.ipk \
  /tmp/prolite-<job>/pkgs/kmod-xxx_....ipk \
  /tmp/prolite-<job>/pkgs/target_....ipk

# 4) 回读
opkg list-installed | grep -E '^<name> - '
opkg info <name>
```

opkg 没有 `--allow-untrusted` 等价开关；是否验签由固件的 `check_signature` 配置决定。**不修改该配置**，验签失败按 `SIGNATURE_INVALID` 报错，并提示用户改用官方源包。

#### 安装失败清理

按“本次新装”集合逆序清理，只删本次引入的包，不碰已装包：

```sh
# apk（逆拓扑序）
apk del <pkgN> ... <pkg1>
# opkg（逆拓扑序）
opkg remove <pkgN> ... <pkg1>

# 恢复配置与清理
tar -xzf /tmp/prolite-<job>/backup-etc-config.tar.gz -C /
rm -rf /tmp/prolite-<job>/pkgs
```

若失败发生在 `apk add` 单条批量命令内部，无法精确知道装了哪些，此时：

1. 用 `before.pkgs` 与当前 `apk info -v` / `opkg list-installed` 求差集；
2. 对差集中的包逆序 remove；
3. 若差集为空但包管理器报错，保留现场并提示手工检查。

### 7.4 dry-run

两级，共用 `PreflightReport`：

1. **桌面 dry-run**：只做 A 段（索引 + 求解 + 空间），可选做 B 段（下载并校验，但不保留）。
2. **设备 dry-run**：上传 → C 段 simulate → 清理。**不执行 S4**，不写包数据库。

```ts
StartInstall({ localPath?, feedPkg?, dryRun: true })
```

### 7.5 取消语义

| 阶段 | 取消行为 |
| --- | --- |
| 下载 | 取消 HTTP 请求、删除 `.part`；已完成的包保留在缓存 |
| S1 | 关闭 SFTP 文件与 session，删除 `.part`，任务结束 |
| S3 | 终止 simulate，删除临时目录 |
| S4 | 发送 SIGINT；若进程未退出则 SIGKILL；随后**强制进入回滚检查**（可能已部分写入） |
| S5+ | 不再允许取消（已进入收尾） |

---

## 8. 结果校验与回滚

### 8.1 安装后回读

对比 S0 与 S5 的快照：

| 对象 | 对比方式 | 判定 |
| --- | --- | --- |
| 包列表 | `before.pkgs` vs `apk info -v` / `list-installed` | `InstallPlan` 中每个包出现且版本正确 |
| init 脚本 | `ls /etc/init.d` 差集 | 新增脚本记为“待启用”，不自动启用 |
| 配置 | `ls /etc/config` + `sha256sum /etc/config/*` | 新增/变化的配置项展示给用户 |
| 空间 | `df -k /overlay` | 记录实际占用，用于校准估算 |
| 进程 | `ps w` 关键词匹配包名 | 仅提示，不作为成功判据 |

成功判据：**包管理器退出码为 0 + 包列表中所有计划包版本正确**。init/config 变化只做展示。

### 8.2 回滚策略

| 场景 | 动作 | 可回滚性 |
| --- | --- | --- |
| 安装前不存在该包，安装失败 | `opkg remove` / `apk del`（逆拓扑序）；恢复 `/etc/config` 备份；删 `/tmp` | 可 |
| 同版本覆盖 | 卸掉后重装旧版本（需旧包文件） | 有条件 |
| 降级 | 需要旧版本包文件；否则不可回滚 | 通常不可 |
| kmod / 内核相关 | **不回滚**，提示重启并人工处理 | 不可 |
| 安装中掉线 | 重连后先做 S5 校验；若包半装，执行 remove + 配置恢复 | 有条件 |
| 覆盖了 `base-files` / `libc` / `busybox` / `dropbear` | 直接阻断，不进入安装 | —— |

明确写进 UI 的“无法回滚”清单：

- 已成功安装并重启后才发现的功能异常（remove 可能带走共用依赖）；
- 安装包修改了 `/etc/rc.d`、`/lib/modules`、`/boot` 的内容；
- 依赖被其他包共用，`remove` 会连带卸载；
- 设备已断网/变砖，桌面端无法再建立 SSH。

### 8.3 备份范围

只备份 `/etc/config`（通常几十 KB），不备份整个 `/etc`（overlay 仅 2.1MB）。备份写 `/tmp`；备份失败时降级为“仅包卸载回滚”，并在报告中标注。

---

## 9. 错误码与前端呈现

### 9.1 错误码

```go
// install.go
type InstallErrorCode string

const (
    // 连接与传输
    ErrNetDisconnected InstallErrorCode = "NET_DISCONNECTED"
    ErrAuth            InstallErrorCode = "AUTH_FAILED"
    ErrHostKey         InstallErrorCode = "HOSTKEY_MISMATCH"
    ErrTransferIO      InstallErrorCode = "TRANSFER_IO"
    ErrHashMismatch    InstallErrorCode = "HASH_MISMATCH"      // 上传后远端校验
    ErrTimeout         InstallErrorCode = "TIMEOUT"
    ErrCancelled       InstallErrorCode = "CANCELLED"

    // 包识别与设备匹配
    ErrFormat          InstallErrorCode = "FORMAT_UNSUPPORTED"
    ErrPkgMgrMismatch  InstallErrorCode = "PKG_MGR_MISMATCH"
    ErrPkgMgrUnknown   InstallErrorCode = "PKG_MGR_UNKNOWN"
    ErrArchMismatch    InstallErrorCode = "ARCH_MISMATCH"
    ErrKernelMismatch  InstallErrorCode = "KERNEL_MISMATCH"
    ErrDangerous       InstallErrorCode = "PKG_DANGEROUS"

    // 桌面端索引与下载（新增）
    ErrIdxUnreachable  InstallErrorCode = "IDX_UNREACHABLE"
    ErrIdxSignature    InstallErrorCode = "IDX_SIGNATURE"
    ErrIdxParse        InstallErrorCode = "IDX_PARSE"
    ErrIdxStale        InstallErrorCode = "IDX_STALE"           // 警告级
    ErrPkgNotFound     InstallErrorCode = "PKG_NOT_FOUND"
    ErrDepUnresolved   InstallErrorCode = "DEP_UNRESOLVED"
    ErrVersionConflict InstallErrorCode = "VERSION_CONFLICT"
    ErrDownloadHash    InstallErrorCode = "DL_HASH"
    ErrDownloadDisk    InstallErrorCode = "DL_DISK"
    ErrDownloadTimeout InstallErrorCode = "DL_TIMEOUT"
    ErrDownloadTooBig  InstallErrorCode = "DL_TOO_LARGE"
    ErrSignature       InstallErrorCode = "SIGNATURE_INVALID"   // 包签名

    // 设备侧
    ErrSpaceOverlay    InstallErrorCode = "SPACE_OVERLAY"
    ErrSpaceTmp        InstallErrorCode = "SPACE_TMP"
    ErrInstallFailed   InstallErrorCode = "INSTALL_FAILED"
    ErrVerifyFailed    InstallErrorCode = "VERIFY_FAILED"
    ErrRollbackFailed  InstallErrorCode = "ROLLBACK_FAILED"
)
```

### 9.2 文案与引导

| 错误码 | 用户文案 | 引导动作 | 可重试 |
| --- | --- | --- | --- |
| `NET_DISCONNECTED` | 与路由器的连接已断开 | 检查网络后重连，任务从上传阶段重来 | 是 |
| `AUTH_FAILED` | SSH 认证失败 | 检查用户名/密钥/密码 | 是 |
| `HOSTKEY_MISMATCH` | 设备主机密钥与记录不一致 | 确认是否重装，必要时重置指纹 | 否 |
| `TRANSFER_IO` | 上传中断 | 点击重试（支持断点续传） | 是 |
| `HASH_MISMATCH` | 上传文件校验失败 | 重试；连续失败检查本地文件 | 是 |
| `FORMAT_UNSUPPORTED` | 不是可安装的 .ipk/.apk | 选择正确的包文件 | 否 |
| `PKG_MGR_MISMATCH` | 设备使用 apk/opkg，与包格式不符 | 下载对应格式的包 | 否 |
| `ARCH_MISMATCH` | 包架构与设备不符 | 下载 mipsel_24kc 版本 | 否 |
| `KERNEL_MISMATCH` | 内核 ABI 不匹配（kmod） | 使用与当前固件同源的 kmod | 否 |
| **`IDX_UNREACHABLE`** | 无法访问索引地址（网络/代理/TLS/404） | 检查桌面网络与代理；或改用本地包 | 是 |
| **`IDX_SIGNATURE`** | 索引签名无效或缺少公钥 | 确认 feed 来源；或显式允许未验证索引 | 是 |
| **`IDX_PARSE`** | 索引格式无法解析 | 切换镜像/刷新；上报诊断 | 是 |
| **`IDX_STALE`** | 索引与设备固件版本可能不一致 | 继续（有风险）或改用同源 feed | 警告 |
| **`PKG_NOT_FOUND`** | 索引中找不到该包（可能已轮转） | 换镜像、指定版本，或改用本地文件 | 是 |
| **`DEP_UNRESOLVED`** | 依赖无法满足：a, b | 刷新索引；补充缺失 feed；或改用本地包 | 是 |
| **`VERSION_CONFLICT`** | 版本冲突：已装 X，目标 Y | 先卸载/升级，或更换版本 | 否 |
| **`DL_HASH`** | 下载文件校验失败 | 重试；多次失败换镜像 | 是 |
| **`DL_DISK`** | 桌面磁盘空间不足 | 清理缓存/换缓存目录 | 是 |
| **`DL_TIMEOUT`** | 下载超时 | 重试或换镜像 | 是 |
| **`DL_TOO_LARGE`** | 下载总量超出限制 | 精简依赖或提高上限 | 否 |
| `SIGNATURE_INVALID` | 包签名不被信任 | 使用官方源包；或显式允许未签名包 | 否 |
| `PKG_DANGEROUS` | 该包含内核/引导组件，已阻止 | M1/M2 不支持此类安装 | 否 |
| `SPACE_OVERLAY` | 存储不足：需要 X，可用 Y | 卸载无用包/扩容 overlay | 是 |
| `SPACE_TMP` | 内存盘空间不足 | 重启释放内存后重试 | 是 |
| `INSTALL_FAILED` | 安装命令失败（附尾部日志） | 查看日志，必要时回滚 | 是 |
| `VERIFY_FAILED` | 安装后未找到目标版本 | 查看日志并回滚 | 否 |
| `ROLLBACK_FAILED` | 回滚失败 | 需通过 SSH 手动处理，附命令 | 否 |

### 9.3 前端呈现

沿用 `frontend/app.js` 的原生渲染与 `state` 模式。

入口：控制中心 → 软件包页顶部“安装包”按钮（`data-act="pkg-install-open"`）。

阶段条（新增“下载”阶段）：

```
选择 → 预检(A) → 下载 → 校验(B) → 上传 → 校验(C) → 安装 → 回读
```

状态：

```js
state.install = {
  open: false,
  phase: 'idle',   // idle|preflight|index|resolve|download|verifyLocal|upload|verifyRemote|simulate|install|verify|done|failed
  files: [],       // { name, size, sent, status: pending|downloading|ok|failed }
  checks: [],      // { id, stage: 'A'|'B'|'C', label, level: ok|warn|fail, detail }
  plan: null,      // InstallPlan（包列表 + 依赖原因）
  log: [],
  dryRun: false,
  jobId: null,
  result: null,
}
```

Wails 事件：

| 事件 | 载荷 |
| --- | --- |
| `install:phase` | `{ jobId, phase, percent }` |
| `install:file` | `{ jobId, index, name, sent, total, status }` |
| `install:log` | `{ jobId, line }` |
| `install:check` | `{ jobId, check }` |
| `install:done` | `{ jobId, result }` |

展示规则：

- 预检失败：清单红色项 + 一句总结，禁用“开始安装”。
- 依赖计划：展示“将安装 N 个包、总大小、其中 X 个为依赖”，可展开每条依赖的引入原因。
- 危险包 / 未签名：二次确认页，显式勾选。
- 失败：错误码、可读文案、尾部日志、“复制诊断”、“回滚”（仅 `rollbackable`）。
- 所有回显走 `esc()`。

---

## 10. 安全边界

| 项 | 设计 |
| --- | --- |
| 权限 | 安装需要 root；与现有 UCI 写操作一致，无 sudo 提权 |
| 设备配置 | **只读**；不写 `/etc/apk/repositories`、`/etc/opkg/*.conf`、不写 `/etc/apk/keys` |
| 危险包 | M1/M2 硬阻断：`kernel`、`kmod-*`、`uboot-*`、`base-files`、`libc`、`busybox`、`dropbear`、`procd`、`netifd`、`firewall4`、`fstools`、`mtd` |
| 内核/引导 | 即使 M3 也不自动安装，只提示走固件升级流程 |
| 二次确认 | 所有非 dry-run 安装都需确认；危险/未签名包需额外勾选 |
| 下载源 | 仅设备已配置 feed；不提供“手填任意 URL”（§12 未决） |
| 索引信任 | 默认必须验签；允许用户显式切换“未验证索引”，UI 常驻警告并写审计 |
| 包信任 | 默认由 apk/opkg 按设备信任根验证；`--allow-untrusted` 仅单文件、显式勾选、写审计 |
| 审计日志 | 追加 JSONL 到 `os.UserConfigDir()/ProLite/install-audit.jsonl`，2MB 轮转 |
| 敏感信息 | 不记录密码、私钥；主机只记 host + 指纹前 16 位 |
| 并发 | 全局单任务锁，避免包管理器锁冲突 |
| 路径注入 | 文件名仅允许 `[A-Za-z0-9._+-]`，禁止 `..`；远端路径固定模板 |
| 命令注入 | 所有参数经 `shq()`（已有实现），不拼接用户输入 |

审计记录示例：

```json
{"ts":"2026-11-19T10:30:00+08:00","host":"192.168.100.1","fp":"SHA256:ab12...","mode":"feed","feed":"https://downloads.immortalwrt.org/...","file":"luci-app-x.apk","sha256":"...","pkg":"luci-app-x","ver":"1.0-r1","arch":"mipsel_24kc","indexVerified":true,"dryRun":false,"allowUntrusted":false,"result":"ok","code":""}
```

---

## 11. 改动清单与里程碑

### 11.1 新增文件

| 文件 | 主要内容 | 预估行数 |
| --- | --- | --- |
| `pkgmeta.go` | `DetectPackage`、`ParseIPK`（ar 解析）、`ParseAPK`（tar.gz）、`PkgMeta`、架构归一化、版本比较 | 350 |
| `feeds.go` | 解析 `/etc/apk/repositories`、`/etc/opkg/*.conf`、URL 归一化、只读采集脚本 | 220 |
| `index.go` | 索引下载、`APKINDEX.tar.gz` / `Packages.gz` 解析、签名验证、缓存/ETag/TTL | 450 |
| `solver.go` | 依赖闭包求解、虚拟包、冲突、kmod ABI、拓扑排序、`InstallPlan` | 480 |
| `download.go` | 并发下载、Range 续传、校验、重试、缓存寻址 | 300 |
| `transfer.go` | `sftpClient`、`UploadPackage`、`execStreamStdin`、进度/续传/清理 | 320 |
| `install.go` | 任务管理、三段预检、离线安装、回读、回滚、错误码 | 700 |
| `install_types.go` | 请求/报告/计划/结果类型 | 160 |
| `*_test.go` | 包解析、索引解析、solver 决策表、错误映射、回滚分支（纯函数，不连设备） | 600 |

### 11.2 修改文件

| 文件 | 修改点 |
| --- | --- |
| `meta.go` | `metaScript` 追加 `@@PKGMGR/@@PKGVER/@@ARCH/@@KERNEL/@@KPKG/@@PKGDBS/@@OVERLAY/@@FEEDS_*/@@KEYS`；`Meta` 增加 `PkgMgr/PkgArch/Kernel/KernelPkg/FeedURLs/APKKeys` |
| `router.go` | 增加 `execJob`、`execStreamStdin`、`installMu`；不改 `execWithClient` |
| `app.go` | 新增 `PickPackageFile`、`InspectPackage`、`ResolvePlan`、`StartInstall`、`CancelInstall`、`GetInstallJob`、`RollbackInstall` |
| `console.go` | 仅新增导出已装包 map 的辅助函数（不改变 `consoleScript` 输出语义） |
| `types.go` | 增加安装相关 JSON 模型 |
| `go.mod` | 增加 `github.com/pkg/sftp`（HTTP 用标准库） |
| `frontend/index.html` | 安装抽屉骨架 |
| `frontend/app.js` | `state.install`、事件订阅、阶段条、依赖计划、进度、结果页 |
| `frontend/styles.css` | 步骤条、检查清单、进度条、日志区 |

### 11.3 关键函数签名

```go
// app.go（Wails 绑定）
func (a *App) PickPackageFile() (string, error)
func (a *App) InspectPackage(path string) (*PkgMeta, error)
func (a *App) ResolvePlan(req InstallRequest) (*InstallPlan, error)
func (a *App) StartInstall(req InstallRequest) (string, error) // 立即返回 jobId
func (a *App) CancelInstall(jobID string) error
func (a *App) GetInstallJob(jobID string) (*InstallJobView, error)
func (a *App) RollbackInstall(jobID string) error

// feeds.go / index.go / solver.go
func (r *Router) ReadFeeds(ctx context.Context) (*FeedConfig, error)
func (r *Router) FetchIndexes(ctx context.Context, cfg *FeedConfig) (*Universe, error)
func Solve(universe *Universe, installed map[string]InstalledPkg,
    roots []RootSpec) (*InstallPlan, error)

// download.go / transfer.go
func (d *Downloader) FetchAll(ctx context.Context, plan *InstallPlan,
    onProgress func(TransferProgress)) error
func (r *Router) UploadPackage(ctx context.Context, localPath, remotePath string,
    onProgress func(TransferProgress)) error

// install.go
func (r *Router) Preflight(ctx context.Context, req InstallRequest) (*PreflightReport, error)
func (r *Router) runInstall(ctx context.Context, job *InstallJob)
func (r *Router) rollback(ctx context.Context, job *InstallJob) error
```

### 11.4 分阶段落地

| 阶段 | 范围 | 验收标准 |
| --- | --- | --- |
| **M1：索引 + 求解 + 下载 + 校验 + 上传 + dry-run** | `feeds.go`、`index.go`、`solver.go`、`download.go`、`pkgmeta.go`、`transfer.go`；前端只读展示 + dry-run | ① 只读解析设备 feed/arch/kernel/已装库，审计证明零写入；② 能下载并验签 `APKINDEX.tar.gz` / `Packages.gz`（或显式 unverified 模式）；③ 对指定包生成完整依赖闭包与拓扑序，依赖原因可展开；④ 并发下载 + 断点续传 + SHA256 校验；⑤ 上传 `/tmp` 并远端 SHA256 一致；⑥ 覆盖 `IDX_UNREACHABLE/IDX_SIGNATURE/PKG_NOT_FOUND/DEP_UNRESOLVED/VERSION_CONFLICT/DL_HASH/DL_DISK` 场景；⑦ 不执行任何安装命令 |
| **M2：离线安装 + 回读** | `install.go` 的 S0–S5；离线 simulate；批量安装 | ① 设备断外网仍能安装成功（可用空 repositories 文件验证）；② 依赖在前、被依赖在后，安装顺序正确；③ 安装后每个计划包版本回读正确；④ 失败时能精确分类并清理本次新装包；⑤ 全程无 `apk update` / `opkg update` |
| **M3：回滚 + 前端完善 + 审计** | 回滚、配置备份恢复、错误引导、审计日志、结果页 | ① 人为制造依赖缺失/空间不足/签名失败，分类正确；② 失败后 `remove` + `/etc/config` 恢复成功；③ 无法回滚场景有明确文案；④ 审计 JSONL 完整、可导出；⑤ 前端阶段条与文件级进度准确 |

M1 完成前不开放真实安装入口，避免“半成品按钮”。

---

## 12. 风险与未决项

### 12.1 需要产品决策

1. **桌面端是否需要外网访问 ImmortalWrt 镜像站？** 必须。若用户处于内网/代理环境，需支持系统代理（`HTTPS_PROXY`）与自定义镜像；完全离线时只能走“本地包文件”模式。
2. **是否允许第三方未签名包？** 建议默认拒绝；允许时需显式勾选 + 单文件 `--allow-untrusted` + 审计。
3. **是否允许手填任意 URL 下载？** 建议不做；若做，必须域名白名单 + HTTPS + 必填 SHA256。
4. **overlay 扩容方案由谁负责？** 2.1MB 是硬约束。可选：外挂 U 盘 overlay、重刷更大分区固件、`extroot`。程序不应替用户改分区。
5. **是否允许安装 kmod？** 建议 M1/M2 一律阻断；kmod 与 snapshot 内核强绑定。
6. **是否允许降级安装？** 建议默认禁止，需显式开关。
7. **安装后是否自动 enable/start 服务？** 建议默认不自动，只在结果页提供“启用并启动”。

### 12.2 技术风险

| 风险 | 影响 | 缓解 |
| --- | --- | --- |
| **桌面端无法访问镜像站**（公司网络/代理/TLS 拦截） | 索引与包都下载不到 | 支持系统代理、自定义镜像、导入本地包；错误码 `IDX_UNREACHABLE` |
| **索引与设备实际已装版本不一致** | solver 误判依赖已满足/版本冲突 | 只读拉取已装库参与求解；比对 `kernelPkg`、`DISTRIB_REVISION`、feed target 路径；不一致给 `IDX_STALE` 并要求确认 |
| snapshot 轮转导致包 404 | 索引里有、下载时已删除 | `PKG_NOT_FOUND` + 建议换镜像；缓存已下载包避免重复 |
| 设备无 `openssh-sftp-server` | SFTP 握手失败 | `cat >` 回退通道（已在方案内） |
| apk 3.x CLI 与旧版差异 | `apk info -v` / `apk version -t` / `--no-network` 不可用 | 版本探测 + 命令回退 + 自实现版本比较 |
| usign 索引签名实现复杂 | 无法在 M1 完成验签 | M1 允许“未验证索引”显式模式；包级签名仍由设备在安装时验证 |
| apk 索引校验和算法前缀不统一 | 下载校验失败或误判 | 按前缀选择算法；无法识别时回退 SHA256 自算 + 记录实测值 |
| `/tmp` 是 tmpfs | 批量上传导致 OOM | 单文件 32MB、批量 64MB 上限 + 空间预检 |
| 安装中断电/断网 | 半装状态 | 重连后校验 + remove 回滚；明确“不可回滚”场景 |
| 两个包管理器共存 | 误用 | 以数据库条目数判定，而非仅看二进制 |
| 备份写 `/tmp` 失败 | 无法恢复配置 | 降级为“仅卸载回滚”，报告标注 |
| Wails 事件丢失 | 前端进度不同步 | `GetInstallJob` 轮询兜底（1s） |

### 12.3 索引漂移检测（具体做法）

在 A 段执行以下比对，任一不符产生 `IDX_STALE`（警告或按配置升级为阻断）：

1. **内核 ABI**：`Meta.KernelPkg`（如 `6.18.26~abcd1234-r1`）与索引中 `kernel` 包版本比对；不一致时所有 kmod 直接阻断。
2. **架构**：`Meta.PkgArch` 与索引条目的 `A:`/`Architecture:` 比对；不一致即无法求解。
3. **固件版本**：`/etc/openwrt_release` 的 `DISTRIB_REVISION` / `DISTRIB_TARGET` 与 feed URL 中的 `targets/<target>/<subtarget>` 比对；snapshot 场景记录抓取时间。
4. **已装包版本**：对计划中每个包，若设备已装版本高于索引版本 → 警告“设备更新，索引可能过期”；若设备已装版本在索引中不存在 → 警告“设备含自编译/已下架包”。
5. **索引时间**：`APKINDEX`/`Packages` 的构建时间与设备 rootfs 构建时间相差超过 7 天 → 提示。

### 12.4 待确认（阻塞 M2 上机）

- MelonPi M21 当前固件到底是 **apk 还是 opkg**（README 与需求描述冲突）。需在设备上执行：

```sh
command -v apk; command -v opkg
apk --print-arch 2>/dev/null
opkg print-architecture 2>/dev/null
uname -r
apk info kernel 2>/dev/null; opkg info kernel 2>/dev/null
df -k /overlay /tmp
```

- 设备是否安装 `openssh-sftp-server`（决定走 SFTP 还是回退通道）。
- 设备 feed 是否可从桌面网络直接访问（代理、镜像、证书）。
- 是否接受“M1 只做 dry-run、不提供真实安装按钮”。

---

## 附录 A：命令速查

| 操作 | opkg | apk |
| --- | --- | --- |
| 架构 | `opkg print-architecture` | `apk --print-arch` |
| 内核包版本 | `opkg info kernel` | `apk info kernel` |
| 源配置 | `cat /etc/opkg/distfeeds.conf` | `cat /etc/apk/repositories` |
| 信任根 | `ls /etc/opkg/keys` | `ls /etc/apk/keys` |
| 桌面拉索引 | `GET <feed>/Packages.gz` + `Packages.sig` | `GET <feed>/APKINDEX.tar.gz` |
| **设备刷新索引** | **禁止** | **禁止** |
| 已装列表 | `opkg list-installed` | `apk info -v`（旧）/ `apk list --installed`（新） |
| 包详情 | `opkg info <name>` | `apk info <name>` |
| 离线 simulate | `opkg install --noaction <files...>` | `apk add --simulate --no-network <files...>` |
| 离线安装 | `opkg install <files...>` | `apk add --no-network <files...>` |
| 卸载 | `opkg remove <name>` | `apk del <name>` |
| 版本比较 | `opkg compare-versions <a> <b>` | `apk version -t <a> <b>` |
| 校验文件 | `sha256sum <file>` | `sha256sum <file>` |
| 剩余空间 | `df -k /overlay /tmp` | `df -k /overlay /tmp` |

## 附录 B：MelonPi M21 适配结论

| 项 | 结论 |
| --- | --- |
| SSH 连接 | 可（现有 TOFU + 密码/密钥已够用） |
| 包管理器 | 以设备实测为准；README 记录为 apk，需复核 |
| 架构 | mipsel_24kc；x86/aarch64 包直接拒绝 |
| kmod | 6.18.x snapshot 滚动，默认阻断；ABI 以 `apk info kernel` / `opkg info kernel` 为准 |
| overlay | ≈2.1MB，基本只能装小型纯脚本包；`luci-app-*` 多数会失败 |
| LuCI 插件 | master 的 ucode/luci-base ABI 敏感，跨 snapshot 组合易缺依赖 |
| 传输 | 优先 SFTP；无 sftp-server 时走 `cat >` 回退 |
| 离线安装 | 设备不联网也可完成（所有依赖已由桌面端下载并上传） |
| 回滚 | 配置可恢复；kmod/内核/引导不可回滚；降级通常不可回滚 |
| 最终判断 | 链路可落地，但该设备上“能装什么”受 overlay 与内核版本强限制，预检必须前置 |
