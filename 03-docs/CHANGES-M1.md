# M1 变更说明（router-prolite）

## 1. 范围

本轮实现 M1：**桌面端下载 + 校验 + 上传 + 离线 dry-run**。

- 已实现：设备包环境只读探测、索引拉取、依赖闭包求解、并发下载与校验、SFTP 上传（无 sftp-server 时回退 `cat >`）、上传后校验、离线 simulate、长任务通道、前端安装页。
- 追加：控制台子标签滚动修复；新增一级入口“插件市场”（仓库/索引/中文说明/分页/预检/dry-run）。
- 未实现（M2）：真实安装、装后回读、回滚、服务启用。真实安装按钮保持禁用并标注“待授权”。
- 未实现（M3）：审计日志、回滚 UI、索引 ETag/更细的签名策略。

## 2. 新增文件

| 文件 | 作用 |
| --- | --- |
| `pkgmeta.go` | 本地解析 `.ipk`（ar + control.tar.gz）与 `.apk`（tar.gz + .PKGINFO），版本比较、架构匹配、依赖表达式拆分 |
| `feeds.go` | 只读采集设备包管理器 / 架构 / 内核 / feed / 已装包 / overlay 与 /tmp 空间 / sftp-server |
| `index.go` | 拉取并解析 `APKINDEX.tar.gz` 与 `Packages.gz`，apk 索引 RSA 验签，HTTP 缓存（6h TTL） |
| `solver.go` | 依赖闭包、虚拟包/Provides、冲突与替换提示、kmod 内核 ABI 检查、拓扑排序 |
| `download.go` | 4 并发下载、HTTP Range 续传、大小与 SHA256/SHA1 校验、3 次重试 |
| `transfer.go` | SFTP 上传，失败自动回退 `cat >` 流式通道；远端大小与 SHA256 校验；临时目录清理 |
| `execjob.go` | 可取消、可超时的长任务通道；SSH stdin 流式上传；不改变现有 `ExecCommand` 行为 |
| `install.go` | 任务管理、三段预检、离线 simulate 命令构建、前端可轮询的任务快照 |
| `install_types.go` | 安装链路 JSON 模型 |
| `pkgmeta_test.go` | 版本比较、依赖拆分、内核 ABI、依赖拓扑、架构预检的单元测试 |

## 3. 修改文件

| 文件 | 修改 |
| --- | --- |
| `app.go` | 增加任务表字段与初始化（`jobsMu` / `jobs`） |
| `go.mod` / `go.sum` | 增加 `github.com/pkg/sftp v1.13.7` |
| `frontend/app.js` | 新增“安装”标签页、环境展示、依赖计划、文件进度、预检清单、日志、轮询、文件选择；真实安装按钮禁用 |
| `frontend/styles.css` | 安装页样式 |
| `docs/install-pipeline-design.md` | 方案文档（从设计稿复制） |

## 4. 行为说明

### 4.0 入口位置

安装页有两个显式入口，均不需要横向滚动：

1. Advanced Lab 顶部主标签行最右侧的 **“安装包”** 按钮（蓝色高亮）；
2. “控制台”子标签行的 **第一个** 标签 **“安装”**。

点击路径：连接设备 → 打开 Advanced Lab → 顶部“安装包”（或“控制台 → 安装”）。

未连接设备时打开该页会显示“未连接设备：安装预检不可用，请先连接路由器”，输入框与按钮禁用，不会空白或报错。

### 4.1 只读采集

安装页进入时执行 `GetPkgEnv`，只使用 `cat` / `ls` / `uname` / `apk info` / `opkg info` 等只读命令：

- 包管理器：同时存在时按 `/lib/apk/db/installed`（`^P:`）与 `/usr/lib/opkg/status`（`^Package:`）条目数选择。
- 架构：`apk --print-arch` / `/etc/apk/arch` / `opkg print-architecture`。
- 内核：`uname -r` 与 `apk info kernel` / `opkg info kernel` 的精确版本（用于 kmod ABI 比对）。
- 源：`/etc/apk/repositories`、`/etc/opkg/distfeeds.conf`、`customfeeds.conf`（不写回）。
- 空间：`df -k /overlay /tmp /`。

### 4.2 下载与求解

- 桌面端访问设备配置的 feed，`APKINDEX.tar.gz` / `Packages.gz` 缓存到 `%AppData%\ProLite\cache\feeds`。
- apk 索引若存在 `.SIGN.RSA*` 成员且有设备公钥，则做 RSA 验签；失败只标记警告（M1 仍允许预演）。
- 依赖求解从目标包出发，已安装且满足约束的依赖不再下载；`kernel` 依赖与设备内核包版本比对；`kmod-*` ABI 不匹配即失败。
- 下载完成逐个包重新解析，校验名称、版本、架构与 SHA256。

### 4.3 预检

预检结论以 `Check` 列表返回，前端按 `ok/warn/fail` 展示：

- 架构、内核 ABI、危险包（kernel/uboot/base-files/libc/busybox/dropbear 等）；
- overlay 空间 = 安装后大小 ×1.125；
- `/tmp` 空间 = 下载总量 ×1.1；
- 冲突 / 替换提示。

任一 `fail` 即终止，不进入上传。

### 4.4 上传

- 目标：`/tmp/prolite-<jobID>/pkgs/`。
- 优先 SFTP（`github.com/pkg/sftp`）；连接失败或设备无 sftp-server 时回退 `cat > file.part && mv`。
- 每文件上传后执行远端 `sha256sum`，与本地 SHA256 比对；没有 `sha256sum` 时退回大小校验。
- `dry-run` 结束后自动删除远端临时目录。

### 4.5 dry-run

- apk：`apk add --simulate --no-network <files>`
- 旧版 apk 不支持 `--no-network` 时回退：`apk --repositories-file /dev/null add --simulate <files>`
- opkg：`opkg install --noaction <files>`

所有依赖包一次性传入，保证离线可解析；**不执行真实安装**，不写包数据库与配置。

### 4.6 长任务

- 新增 `Router.execJob(ctx, cmd, timeout, onLine)`：`sess.Start` + 逐行回调 + `SIGINT`/`Close` 取消。
- `ExecCommand` 保持原样（25 秒 / 2000 字符限制不受影响）。
- 任务默认 20 分钟超时，可从前端取消。

## 5. 错误码（本轮实际使用）

| 错误码 | 场景 |
| --- | --- |
| `NET_READ_ENV` | 读取设备包环境失败 / 未连接 |
| `PKG_MGR_UNKNOWN` | 未检测到 apk 或 opkg |
| `ARCH_UNKNOWN` | 无法读取设备架构 |
| `IDX_UNREACHABLE` | 所有索引不可用 |
| `PKG_NOT_FOUND` | 目标包不在索引中 |
| `DEP_UNRESOLVED` | 依赖闭包无法满足 / 依赖环 |
| `FORMAT_UNSUPPORTED` | 本地文件不是 ipk/apk 或解析失败 |
| `PKG_MGR_MISMATCH` | 包格式与设备包管理器不符 |
| `ARCH_MISMATCH` | 架构不匹配 |
| `DL_FAILED` / `DL_HASH` | 下载失败 / 校验不一致 |
| `TRANSFER_IO` | 上传或远端校验失败 |
| `INSTALL_FAILED` | simulate 失败 |
| `CANCELLED` | 任务被取消（通过任务状态体现） |

## 6. 已知限制

1. **真实安装未开放**：前端按钮禁用；后端 `StartInstallJob` 在 `dryRun=false` 时直接报错。
2. **opkg 索引签名未实现**：M1 只依赖每个包的 `SHA256sum` 与设备安装阶段的 opkg 验签。
3. **apk v3 CLI 适配有限**：`apk info -v` 为空时回退 `apk list --installed`，但后者的输出格式未做完整解析。
4. **本地包 + 无 feed**：若本地包无依赖，可离线预演；若有依赖且设备没有可用 feed，求解会失败。
5. **不做版本降级/覆盖**：已安装同版本或更高版本时直接拒绝。
6. **kmod 不一律阻断**：仅在 ABI 不匹配时失败；M2 建议改为默认阻断。
7. **索引缓存不处理 ETag**：6 小时 TTL，失败时使用过期缓存。
8. **未做设备实测**：本轮未连接任何设备；索引解析、上传、simulate 的真机行为待 M2 前验证。

## 7. 构建与验证

本机未安装 Go / Wails CLI，使用便携版 Go 1.27.4 编译：

```powershell
go build -mod=mod -tags desktop,production -ldflags "-H windowsgui -s -w" -o build\bin\ProLite.exe .
```

- `go test ./...`：通过
- `go vet ./...`：通过
- 产物：`build\bin\ProLite.exe`（约 13.8MB）
- WebView2 Runtime 已安装；未使用 `-webview2 embed`（会把运行时打进 exe，体积增加约 150MB）。

项目无 `wails.json`，前端通过 `//go:embed all:frontend` 内嵌，README 也说明无需 Wails CLI，因此采用 `go build` 作为官方构建路径。

## 8. 追加：控制台滚动条与插件市场

### 8.1 控制台子标签滚动

- 结构改为 `左箭头 + .ctabs 滚动区 + 右箭头`。
- `.ctabs` 使用 `scrollbar-width: thin` 与 `::-webkit-scrollbar { height: 6px }`，滚动条始终可见；左右仍有内容时用渐变遮罩提示。
- 支持鼠标滚轮纵向滚动映射为横向，触摸滑动使用原生 `overflow-x:auto`。
- 箭头每次滚动 80% 可视宽度（至少 140px），无更多内容时置灰禁用。
- 重新渲染后自动把当前 `.chip.active` 滚入可视区域；窗口缩放时重新计算箭头状态。

### 8.2 插件市场

- 入口：Advanced Lab 顶部主标签行第 2 个 **“插件市场”**（绿色高亮）；未连接设备时，连接页底部有 **“插件市场（离线浏览）”**。
- 点击路径：连接设备 → 打开 Advanced Lab → 顶部“插件市场”；离线：启动页 → “插件市场（离线浏览）”。
- 仓库地址：默认通过 `GetMarketRepos` 只读带出设备的 `/etc/apk/repositories` 或 `/etc/opkg/distfeeds.conf`；可手动多行编辑、保存到 `localStorage`（键 `prolite-market-repos`）。
- 拉取索引：`GetMarketIndex` 在桌面端逐个仓库尝试 `APKINDEX.tar.gz`，失败再尝试 `Packages.gz`/`Packages`；每个仓库单独返回 HTTP 状态与失败原因。索引不会让设备联网。
- 列表：名称、版本、体积、是否已安装、简短说明；支持搜索、只看未安装、按名称/体积排序；前端每页 50 条，避免一次渲染数千行。
- 中文说明：`frontend/market-i18n.js` 内置 70 个包名的中文一句话说明与用途，未收录的包回退索引 `Description` 并显示“未翻译”标签，不编造译文。
- 详情：依赖列表、下载/安装体积、许可证、分类、来源仓库、主页；未提供时显示“索引未提供”。
- 加入计划：`ResolvePlan` 使用设备环境 + 自定义仓库做依赖求解与预检，返回依赖树与 `ok/warn/fail` 清单；危险包标记 `dangerous=true`。
- dry-run：`StartInstallJob`（`dryRun=true`，携带 `repos`）执行 下载 → 本地校验 → 上传 → 设备端 simulate；不写入包数据库与配置。
- 真实安装：界面按钮显示 **“安装到设备（待授权）”**，保持禁用；后端 `StartInstallJob` 仍拒绝 `dryRun=false`。本轮未开启设备写入通道。
- 设备写入授权（预留）：`%AppData%\ProLite\device-write-allow.json` 用于绑定目标设备、允许 overlay 与 /tmp、是否允许危险包；文件不存在时默认禁止真实安装。`devwrite.go` 已实现该校验，`install_exec.go` 已实现安装执行、回读与日志，但启动开关尚未接通。
- 落盘日志：每个任务写入 `%AppData%\ProLite\logs\install-<jobID>.log`，包含命令、退出码与输出尾部。
- 未连接设备：页面可打开，顶部提示“未连接设备：无法读取默认仓库，也无法做依赖预检”；仍可手动填写仓库并拉取索引浏览。

### 8.3 新增错误码

| 错误码 | 场景 |
| --- | --- |
| `SPACE_OVERLAY` | overlay 空间不足，直接禁止安装 |
| `SPACE_TMP` | /tmp 空间不足以存放上传包 |
| `KERNEL_MISMATCH` | kmod 要求的内核 ABI 与设备不符 |
| `DANGER_CONFIRM` | 计划包含危险包且未做额外确认（真实安装通道启用后生效） |
| `REPO_INVALID` | 自定义仓库地址为空或格式无效 |

### 8.5 apk v3 ADB 索引支持（2026-11-19 追加）

- 真机环境：ImmortalWrt SNAPSHOT、apk-tools 3.0.5、`/etc/apk/repositories.d/*.list`、索引 `packages.adb`（magic `ADBd`），旧 `APKINDEX.tar.gz` 返回 404。
- 新增 [adbindex.go](file:///C:/Users/13223/Documents/Qoder/2026-11-19/fd2f4a9e/router-prolite/adbindex.go)：
  - 按文件头第 4 字节识别压缩：`.` 无压缩、`d` 原始 DEFLATE、`c` 算法/级别（Deflate 支持，Zstd 明确报不支持）。
  - 解析 `ADB.` + `indx` schema 的块结构，读取包名、版本、架构、依赖、Provides、Provides/Replaces、安装体积、下载体积、描述、许可证、origin、hash。
  - 下载地址按 `<feed>/<name>-<version>.apk` 生成。`hashes` 字段经真机验证等于包内 ADB 元数据块 payload 的 SHA256（不是整个 `.apk` 文件），下载校验按 `ChecksumKind=adb-payload` 解析包内块后再比对。
  - 检测到 SIG 块时在结果中标注“未验签”；ADB 签名是 ECDSA/PEM，现有 RSA 路径不适用。
- 探测顺序：`packages.adb` → `APKINDEX.tar.gz` → `Packages.gz` / `Packages`；全部失败时逐仓库返回三段错误（含 HTTP 状态）。回退成功时也会在备注中说明 ADB 失败原因，不静默降级。
- 仓库地址支持：目录、显式 `packages.adb`、显式 `APKINDEX.tar.gz`、`$arch` / `${arch}` 替换、多行多仓库、opkg `src/gz` 行。
- 真机只读验证：8 个仓库中 6 个可用，去重后 9510 个包名；`curl 8.23.0-r1`、`libc 1.2.6-r5` 与设备已装版本一致；`busybox`/`base-files` 因 snapshot 漂移存在 revision 差异。
- 离线安装命令主路径改为 `apk --repositories-file /dev/null add <file>`；`--no-network` 仅保留为旧版 apk 回退分支。

### 8.6 已知限制（新增）

1. ADB SIG 块是 ECDSA/PEM 签名，本轮只读解析未验签，仅标注提示。
2. `packages.adb` 的 Zstd 压缩（`c` 头 + alg=2）未实现，会返回明确错误。
3. snapshot 索引与设备实际固件可能不同步（已观察到 busybox/base-files revision 差异），依赖求解可能选到比设备更新的版本。
4. 两个设备仓库（qmodem、video）在当前 snapshot 返回 404，市场会按仓库逐条显示。
5. 真实安装开关仍保持关闭，本轮未写设备。
6. 中文说明为人工维护的常用插件表，覆盖率有限；未收录包明确标注“未翻译”。
7. 索引一次最多处理 8 个仓库；列表分页 50 条/页，未使用虚拟滚动。
8. 插件市场拉取索引时强制刷新（不使用 6 小时缓存），网络慢时按钮会持续显示“正在拉取索引…”。
