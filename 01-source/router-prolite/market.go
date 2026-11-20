package main

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
)

// MarketEnvView 是插件市场页面需要的设备环境（未连接时也能返回）。
type MarketEnvView struct {
	Connected bool   `json:"connected"`
	Manager   string `json:"manager"`
	Arch      string `json:"arch"`
	Kernel    string `json:"kernel"`
	Release   string `json:"release"`
	Feeds     []Feed `json:"feeds"`
	Installed int    `json:"installed"`
	OverlayKB int64  `json:"overlayKb"`
	Note      string `json:"note"`
}

// MarketPkg 是插件市场列表中的一条包。
type MarketPkg struct {
	Name             string   `json:"name"`
	Version          string   `json:"version"`
	Arch             string   `json:"arch"`
	Size             int64    `json:"size"`
	InstalledSize    int64    `json:"installedSize"`
	Description      string   `json:"description"`
	Zh               string   `json:"zh,omitempty"`
	License          string   `json:"license"`
	Section          string   `json:"section"`
	Origin           string   `json:"origin"`
	Homepage         string   `json:"homepage"`
	Feed             string   `json:"feed"`
	Filename         string   `json:"filename"`
	Depends          []string `json:"depends"`
	Installed        bool     `json:"installed"`
	InstalledVersion string   `json:"installedVersion"`
}

// MarketIndexView 是一次索引拉取的结果。
type MarketIndexView struct {
	Packages  []*MarketPkg `json:"packages"`
	Checks    []Check      `json:"checks"`
	Sources   []string     `json:"sources"`
	Total     int          `json:"total"`
	Connected bool         `json:"connected"`
	Manager   string       `json:"manager"`
	Arch      string       `json:"arch"`
	ElapsedMs int64        `json:"elapsedMs"`
}

// MarketResolveRequest 是“加入计划 / 预检”的输入。
type MarketResolveRequest struct {
	Mode    string   `json:"mode"`
	Package string   `json:"package"`
	Version string   `json:"version"`
	Repos   []string `json:"repos"`
}

// MarketPlanView 是加入计划后的依赖树与预检结论（不下载、不上传）。
type MarketPlanView struct {
	Env    *PkgEnv      `json:"env"`
	Plan   *InstallPlan `json:"plan"`
	Checks []Check      `json:"checks"`
}

// GetMarketRepos 返回设备只读带出的仓库；未连接时返回空列表与提示，不报错。
func (a *App) GetMarketRepos() *MarketEnvView {
	view := &MarketEnvView{Feeds: []Feed{}}
	env, err := a.router.ReadPkgEnv()
	if err != nil || env == nil {
		view.Note = "未连接设备：无法读取 /etc/apk/repositories、/etc/apk/repositories.d/* 或 /etc/opkg/distfeeds.conf，请手动填写仓库地址（仍可拉取索引）。"
		return view
	}
	view.Connected = true
	view.Manager = env.Manager
	view.Arch = env.Arch
	view.Kernel = env.Kernel
	view.Release = env.Release
	view.Feeds = env.Feeds
	view.Installed = len(env.Installed)
	view.OverlayKB = env.OverlayKB
	if len(env.Feeds) == 0 {
		view.Note = "设备没有可用的包源配置，请手动填写仓库地址。"
	}
	return view
}

// GetMarketIndex 从给定仓库（或设备源）拉取索引，返回全部包的可浏览列表。
// 不依赖设备连接；失败时在 Checks 中给出每个仓库的 HTTP 状态与原因。
func (a *App) GetMarketIndex(repos []string) (*MarketIndexView, error) {
	start := time.Now()
	view := &MarketIndexView{Packages: []*MarketPkg{}, Checks: []Check{}, Sources: []string{}}

	env, envErr := a.router.ReadPkgEnv()
	installed := map[string]InstalledPkg{}
	arch := ""
	if envErr == nil && env != nil {
		view.Connected = true
		view.Manager = env.Manager
		view.Arch = env.Arch
		arch = env.Arch
		installed = env.Installed
	}
	feeds := parseRepoFeedsForArch(repos, arch)
	if len(feeds) == 0 && env != nil {
		feeds = env.Feeds
		for i := range feeds {
			feeds[i].URL = expandRepoVars(feeds[i].URL, arch)
		}
	}
	if len(feeds) == 0 {
		return nil, fmt.Errorf("未连接设备且没有填写仓库地址：请至少输入一个 feed 地址")
	}
	if len(feeds) > 8 {
		feeds = feeds[:8]
		view.Checks = append(view.Checks, Check{ID: "MKT-LIMIT", Stage: "index", Label: "仓库数量",
			Level: "warn", Detail: "一次最多拉取 8 个仓库，多余地址已忽略"})
	}

	seen := map[string]bool{}
	for i, feed := range feeds {
		id := fmt.Sprintf("MKT-%d", i+1)
		srcStart := time.Now()
		entries, note, err := a.router.fetchAnyIndex(feed, 0)
		view.Sources = append(view.Sources, feed.URL)
		if err != nil {
			view.Checks = append(view.Checks, Check{ID: id, Stage: "index", Label: feed.Name,
				Level: "fail", Detail: fmt.Sprintf("%s 拉取失败：%s", feed.URL, err.Error())})
			continue
		}
		added := 0
		for _, p := range entries {
			key := p.Name + "\x00" + p.Version
			if seen[key] {
				continue
			}
			seen[key] = true
			mp := &MarketPkg{
				Name: p.Name, Version: p.Version, Arch: p.Arch,
				Size: p.Size, InstalledSize: p.InstalledSize,
				Description: p.Description, Zh: marketZhText(p.Name, p.Version), License: p.License, Section: p.Section,
				Origin: p.Origin, Homepage: p.Homepage, Feed: p.Feed, Filename: p.Filename,
				Depends: p.Depends,
			}
			if inst, ok := installed[p.Name]; ok {
				mp.Installed = true
				mp.InstalledVersion = inst.Version
			}
			view.Packages = append(view.Packages, mp)
			added++
		}
		level := "ok"
		detail := fmt.Sprintf("%d 个包，用时 %s", added, time.Since(srcStart).Round(time.Millisecond))
		if note != "" {
			detail += "；" + note
			level = "warn"
		}
		view.Checks = append(view.Checks, Check{ID: id, Stage: "index", Label: feed.Name, Level: level, Detail: detail})
	}

	sort.SliceStable(view.Packages, func(i, j int) bool {
		if view.Packages[i].Name == view.Packages[j].Name {
			return compareVersions(view.Packages[i].Version, view.Packages[j].Version) > 0
		}
		return view.Packages[i].Name < view.Packages[j].Name
	})
	view.Total = len(view.Packages)
	view.ElapsedMs = time.Since(start).Milliseconds()
	if view.Total == 0 {
		view.Checks = append(view.Checks, Check{ID: "MKT-EMPTY", Stage: "index", Label: "索引",
			Level: "fail", Detail: "所有仓库都没有解析到包，请检查地址是否指向 feed 根目录"})
	}
	return view, nil
}

// ResolvePlan 用设备环境 + 索引做依赖求解与元数据预检；不下载、不上传。
func (a *App) ResolvePlan(req MarketResolveRequest) (*MarketPlanView, error) {
	pkg := strings.TrimSpace(req.Package)
	if pkg == "" {
		return nil, fmt.Errorf("请选择要安装的包")
	}
	env, err := a.router.ReadPkgEnv()
	if err != nil || env == nil {
		return nil, fmt.Errorf("未连接设备：无法做依赖预检")
	}
	if env.Manager == "" {
		return nil, fmt.Errorf("设备未检测到 apk 或 opkg，无法预检")
	}
	if feeds := parseRepoFeedsForArch(req.Repos, env.Arch); len(feeds) > 0 {
		env.Feeds = feeds
	} else {
		for i := range env.Feeds {
			env.Feeds[i].URL = expandRepoVars(env.Feeds[i].URL, env.Arch)
		}
	}
	if len(env.Feeds) == 0 {
		return nil, fmt.Errorf("没有可用仓库地址")
	}
	u, checks, err := a.router.FetchUniverse(env, nil)
	if err != nil {
		return nil, err
	}
	root, err := RootItemFromIndex(u, pkg, strings.TrimSpace(req.Version))
	if err != nil {
		return nil, err
	}
	plan, solveChecks, err := Solve(u, env, root)
	checks = append(checks, solveChecks...)
	if err != nil {
		return nil, err
	}
	checks = append(checks, preflightPlan(plan, env)...)
	return &MarketPlanView{Env: env, Plan: plan, Checks: checks}, nil
}

// parseRepoFeeds 解析用户输入的仓库地址：每行一个 URL，也接受 opkg 的 src/gz 行与 @tag URL。
func parseRepoFeeds(lines []string) []Feed {
	return parseRepoFeedsForArch(lines, "")
}

// parseRepoFeedsForArch 额外展开 $arch / ${arch}，并保留显式的 packages.adb 地址。
func parseRepoFeedsForArch(lines []string, arch string) []Feed {
	out := []Feed{}
	seen := map[string]bool{}
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.Index(line, " #"); i > 0 {
			line = strings.TrimSpace(line[:i])
		}
		name, url := "", ""
		fields := strings.Fields(line)
		switch {
		case len(fields) >= 3 && (fields[0] == "src/gz" || fields[0] == "src"):
			name, url = fields[1], fields[2]
		case len(fields) == 2 && strings.HasPrefix(fields[1], "http"):
			name, url = strings.TrimPrefix(fields[0], "@"), fields[1]
		case len(fields) >= 1:
			url = fields[0]
		}
		url = expandRepoVars(url, arch)
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			continue
		}
		url = strings.TrimRight(url, "/")
		if seen[url] {
			continue
		}
		seen[url] = true
		if name == "" {
			name = path.Base(url)
		}
		out = append(out, Feed{Name: name, URL: url})
	}
	return out
}
