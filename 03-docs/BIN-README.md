# ProLite M1 运行说明

## 运行

1. 双击 `ProLite.exe`。
2. 系统要求：Windows 10/11 x64，已安装 **Microsoft Edge WebView2 Runtime**（Win11 默认自带；缺失时从微软官网安装）。
3. 程序为单文件，无需安装 Go、Wails 或 Node。

## 使用安装预演（M1）

1. 连接路由器（SSH）。
2. 打开安装页，两种入口任选：
   - 顶部主标签行最右侧的蓝色 **“安装包”** 按钮；
   - 或“控制台 → 第一个子标签 **安装**”。
3. 查看设备包环境：包管理器、架构、内核、feed、overlay 与 /tmp 空间、SFTP 状态。
4. 选择模式：
   - “从设备源下载”：输入包名，桌面端从设备已配置的 feed 下载依赖闭包。
   - “本地包文件”：选择 `.ipk` / `.apk`，桌面端解析元数据并从 feed 补齐依赖。
5. 点击“开始 dry-run 预演”。
6. 查看预检结论、依赖计划、下载/上传进度与 simulate 日志。

## 使用插件市场

1. 连接路由器后，打开 Advanced Lab（右上角 Pro 或 Ctrl+K）。
2. 顶部主标签第 2 个是 **“插件市场”**；未连接设备时也可从启动页底部 **“插件市场（离线浏览）”** 进入。
3. 仓库地址默认只读带出设备的 `/etc/apk/repositories`、`/etc/apk/repositories.d/*`（新版 apk 布局）或 `/etc/opkg/distfeeds.conf`；可手动多行修改并“保存地址”。
4. 点击“测试连通性 / 拉取索引”，失败时每个仓库会显示 HTTP 状态与原因。支持 apk v3 的 `packages.adb`（ADBd）以及旧 `APKINDEX.tar.gz` / opkg `Packages.gz`。
5. 在列表中搜索、筛选、排序（每页 50 条），点击一行查看依赖、体积、许可证与来源。
6. 点击“加入计划 / 预检”查看依赖树与预检结论；危险包会标注，overlay 空间不足会直接判定不可安装。
7. 点击“执行 dry-run 预演”完成 下载 → 校验 → 上传 → 设备端 simulate。预演通过后，在“插件市场”精选卡片中点击“确认写入设备”，逐字输入当前设备地址并勾选写入确认，即可真实安装；需要本机存在 `%AppData%\ProLite\device-write-allow.json` 且 `host` 与当前设备一致。

## 控制台子标签

窗口变窄时子标签行显示细滚动条，并保留左右箭头；可用鼠标滚轮横向滚动，当前标签会自动滚入可视区。

## 安全边界

- 本轮**不会真实安装**；“真实安装（待 M2）”按钮为禁用状态。
- 上传只写 `/tmp/prolite-<jobID>/`，预演结束自动删除。
- 不修改设备 feed 配置、不执行 `apk update` / `opkg update`。
- 设备端全程离线，只使用已上传的本地包。

## 缓存

- feed 索引与已下载包：`%AppData%\ProLite\cache\`
- 日志：`%AppData%\ProLite\prolite.log`

## 已知限制

- 真实安装、装后回读、回滚属于 M2/M3。
- overlay 仅 2.1MB 时，大多数插件会在预检阶段被拒绝（空间不足），这是预期行为。
- kmod 与内核 ABI 不匹配会被拒绝。
- opkg 索引签名未在桌面端校验（包 SHA256 与设备 opkg 验签仍然生效）。
