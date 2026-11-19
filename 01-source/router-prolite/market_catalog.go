package main

import (
	"fmt"
	"sort"
	"strings"
)

// 插件市场只收录 noarch 的 LuCI / 脚本类插件。
// 目录随程序版本发布更新；版本、依赖和体积在预演时从设备自身的 apk/opkg 源实时解析，
// 因此不需要远程动态清单，也不会因为清单失联而让界面不可用。
const (
	marketMaxPkgInstall   = int64(4 << 20)  // 单包安装后体积上限 4 MiB
	marketMaxTotalInstall = int64(12 << 20) // 整单安装后体积上限 12 MiB
)

type PluginCatalogEntry struct {
	ID          string `json:"id"`
	Package     string `json:"package"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Category    string `json:"category"`
}

type PluginMarketItem struct {
	PluginCatalogEntry
	Installed        bool   `json:"installed"`
	InstalledVersion string `json:"installedVersion"`
}

type PluginMarketLimits struct {
	MaxPkgInstall   int64 `json:"maxPkgInstall"`
	MaxTotalInstall int64 `json:"maxTotalInstall"`
}

type PluginMarketView struct {
	Connected bool                   `json:"connected"`
	Host      string                 `json:"host"`
	Manager   string                 `json:"manager"`
	Arch      string                 `json:"arch"`
	Kernel    string                 `json:"kernel"`
	Release   string                 `json:"release"`
	OverlayKB int64                  `json:"overlayKb"`
	TmpKB     int64                  `json:"tmpKb"`
	Installed int                    `json:"installed"`
	Plugins   []PluginMarketItem     `json:"plugins"`
	Limits    PluginMarketLimits     `json:"limits"`
	Policy    *DeviceWritePolicyView `json:"policy"`
	Note      string                 `json:"note"`
}

var marketCatalog = []PluginCatalogEntry{
	{ID: "commands", Package: "luci-app-commands", Title: "自定义命令", Category: "工具",
		Description: "在 LuCI 中添加自定义 Shell 命令按钮，适合巡检、重启服务等常用操作。"},
	{ID: "filetransfer", Package: "luci-app-filetransfer", Title: "文件传输", Category: "工具",
		Description: "通过网页上传、下载路由器文件，免去额外开 SFTP 客户端。"},
	{ID: "wifischedule", Package: "luci-app-wifischedule", Title: "Wi-Fi 定时开关", Category: "网络",
		Description: "按时间段自动开启或关闭无线，适合夜间定时关闭 Wi-Fi。"},
	{ID: "sqm", Package: "luci-app-sqm", Title: "SQM 智能队列", Category: "网络",
		Description: "用 CAKE/fq_codel 抗缓冲膨胀，改善下载时的游戏与视频通话延迟。"},
	{ID: "ddns", Package: "luci-app-ddns", Title: "动态 DNS", Category: "网络",
		Description: "把动态公网 IP 绑定到域名，方便远程访问路由器或内网服务。"},
	{ID: "mwan3", Package: "luci-app-mwan3", Title: "MWAN3 多线", Category: "网络",
		Description: "多 WAN 负载均衡与故障切换，适合双宽带或宽带 + 4G 备份。"},
	{ID: "banip", Package: "luci-app-banip", Title: "banIP 黑名单", Category: "安全",
		Description: "基于 nftables 的 IP 黑名单，支持订阅公开威胁情报列表。"},
	{ID: "attendedsysupgrade", Package: "luci-app-attendedsysupgrade", Title: "在线升级", Category: "系统",
		Description: "在保留已安装插件的前提下生成并刷写固件（Attended Sysupgrade）。"},
	{ID: "uhttpd", Package: "luci-app-uhttpd", Title: "Web 服务器设置", Category: "服务",
		Description: "调整 LuCI 所用 uhttpd 的端口、证书与监听地址。"},
	{ID: "firewall", Package: "luci-app-firewall", Title: "防火墙设置", Category: "网络",
		Description: "图形化维护区域、转发规则与端口开放；多数固件已预装。"},
	{ID: "opkg", Package: "luci-app-opkg", Title: "软件包管理（opkg）", Category: "系统",
		Description: "opkg 固件上的图形化软件包管理界面；apk 固件请选择下一项。"},
	{ID: "package-manager", Package: "luci-app-package-manager", Title: "软件包管理（apk）", Category: "系统",
		Description: "apk 固件上的图形化软件包管理界面；opkg 固件请选择上一项。"},
}

func marketCatalogByID(id string) (PluginCatalogEntry, bool) {
	id = strings.TrimSpace(id)
	for _, e := range marketCatalog {
		if e.ID == id {
			return e, true
		}
	}
	return PluginCatalogEntry{}, false
}

// isNoarch 判断包是否为脚本/LuCI 类（与具体 CPU 架构无关）。
func isNoarch(arch string) bool {
	switch strings.ToLower(strings.TrimSpace(arch)) {
	case "", "all", "noarch", "any", "*":
		return true
	}
	return false
}

// GetPluginMarket 返回内置精选插件列表与设备环境；不拉取完整索引，打开页面即可浏览。
func (a *App) GetPluginMarket() *PluginMarketView {
	view := &PluginMarketView{
		Plugins: []PluginMarketItem{},
		Limits: PluginMarketLimits{
			MaxPkgInstall:   marketMaxPkgInstall,
			MaxTotalInstall: marketMaxTotalInstall,
		},
		Policy: a.GetDeviceWritePolicy(),
	}
	env, err := a.router.ReadPkgEnv()
	if err != nil || env == nil {
		view.Note = "未连接设备：连接路由器后才能读取已安装状态并执行预演。"
		return view
	}
	view.Connected = true
	view.Host = a.router.TargetHost()
	view.Manager = env.Manager
	view.Arch = env.Arch
	view.Kernel = env.Kernel
	view.Release = env.Release
	view.OverlayKB = env.OverlayKB
	view.TmpKB = env.TmpKB
	view.Installed = len(env.Installed)
	if len(env.Feeds) == 0 {
		view.Note = "设备没有可用的包源配置，预演会失败；请先在路由器上配置 apk/opkg 源。"
	}
	for _, entry := range marketCatalog {
		item := PluginMarketItem{PluginCatalogEntry: entry}
		if inst, ok := env.Installed[entry.Package]; ok {
			item.Installed = true
			item.InstalledVersion = inst.Version
		}
		view.Plugins = append(view.Plugins, item)
	}
	sort.SliceStable(view.Plugins, func(i, j int) bool {
		if view.Plugins[i].Category == view.Plugins[j].Category {
			return view.Plugins[i].Title < view.Plugins[j].Title
		}
		return view.Plugins[i].Category < view.Plugins[j].Category
	})
	return view
}

// marketPlanChecks 对插件市场的依赖闭包做硬约束校验：
// 只允许 noarch、非危险包、单包与整单体积不超限。任一项 fail 都不会下载或写入。
func marketPlanChecks(plan *InstallPlan) []Check {
	checks := []Check{}
	total := int64(0)
	pending := 0
	for _, it := range plan.Packages {
		if it.Installed {
			continue
		}
		pending++
		total += it.InstalledSize
		if !isNoarch(it.Arch) {
			checks = append(checks, Check{
				ID: "MARKET-ARCH-" + it.Name, Stage: "A", Label: "非 noarch 包 " + it.Name, Level: "fail",
				Detail: fmt.Sprintf("架构 %s 属于编译型二进制或驱动；本市场只允许 noarch 脚本/LuCI 包", orDash(it.Arch)),
			})
		}
		if dangerousPackage(it.Name) {
			checks = append(checks, Check{
				ID: "MARKET-DANGER-" + it.Name, Stage: "A", Label: "禁止安装 " + it.Name, Level: "fail",
				Detail: "属于 kmod/内核/引导/基础系统包，插件市场不提供安装入口",
			})
		}
		if it.InstalledSize > marketMaxPkgInstall || it.Size > marketMaxPkgInstall {
			checks = append(checks, Check{
				ID: "MARKET-SIZE-" + it.Name, Stage: "A", Label: "大体积包 " + it.Name, Level: "fail",
				Detail: fmt.Sprintf("下载 %s / 安装后 %s，超过单包 %s 上限；大体积插件请用高级模式手动处理",
					humanBytes(it.Size), humanBytes(it.InstalledSize), humanBytes(marketMaxPkgInstall)),
			})
		}
	}
	if total > marketMaxTotalInstall {
		checks = append(checks, Check{
			ID: "MARKET-SIZE-TOTAL", Stage: "A", Label: "整单体积超限", Level: "fail",
			Detail: fmt.Sprintf("安装后约 %s，超过整单 %s 上限", humanBytes(total), humanBytes(marketMaxTotalInstall)),
		})
	}
	if len(checks) == 0 {
		checks = append(checks, Check{
			ID: "MARKET-LIMITS", Stage: "A", Label: "市场约束通过", Level: "ok",
			Detail: fmt.Sprintf("%d 个待安装包，安装后约 %s；全部为 noarch 且未超体积上限", pending, humanBytes(total)),
		})
	}
	return checks
}
