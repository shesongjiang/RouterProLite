package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func waitJob(t *testing.T, a *App, id string, timeout time.Duration) *InstallJobView {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		view, err := a.GetInstallJob(id)
		if err != nil {
			t.Fatalf("get job: %v", err)
		}
		if !view.Running {
			return view
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s timeout; last phase=%s", id, view.Phase)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// TestLiveMarketIndex 直接走插件市场页面的数据入口，验证设备源能被完整拉取。
func TestLiveMarketIndex(t *testing.T) {
	if os.Getenv("PROLITE_LIVE") != "1" {
		t.Skip("set PROLITE_LIVE=1 to run against the real router")
	}
	r, _ := liveDial(t)
	a := &App{router: r}
	view, err := a.GetMarketIndex(nil)
	if err != nil {
		t.Fatalf("market index: %v", err)
	}
	installed, withDesc := 0, 0
	cats := map[string]bool{}
	for _, p := range view.Packages {
		if p.Installed {
			installed++
		}
		if p.Description != "" {
			withDesc++
		}
		cats[p.Section] = true
	}
	t.Logf("market total=%d installed=%d withDesc=%d sections=%d sources=%d elapsed=%dms",
		view.Total, installed, withDesc, len(cats), len(view.Sources), view.ElapsedMs)
	for _, c := range view.Checks {
		t.Logf("index %-6s %-12s %-4s %s", c.ID, c.Label, c.Level, c.Detail)
	}
	if view.Total < 1000 {
		t.Fatalf("包数量异常：%d", view.Total)
	}
	if installed == 0 {
		t.Fatalf("没有标记已安装包")
	}
}

func TestLiveMarketCandidates(t *testing.T) {
	if os.Getenv("PROLITE_LIVE") != "1" {
		t.Skip("set PROLITE_LIVE=1 to run against the real router")
	}
	r, _ := liveDial(t)
	env, err := r.ReadPkgEnv()
	if err != nil {
		t.Fatalf("read pkg env: %v", err)
	}
	u, checks, err := r.FetchUniverse(env, func(s string) { t.Logf("fetch: %s", s) })
	if err != nil {
		t.Fatalf("universe: %v", err)
	}
	for _, c := range checks {
		t.Logf("index %-6s %-28s %-4s %s", c.ID, c.Label, c.Level, c.Detail)
	}
	for _, name := range []string{"kmod-ifb", "kmod-sched-core", "tc", "sqm-scripts"} {
		list := u.ByName[name]
		t.Logf("universe %-16s hits=%d providers=%d", name, len(list), len(u.Providers[name]))
		for _, p := range list {
			t.Logf("  %s %s arch=%s size=%d", p.Name, p.Version, p.Arch, p.Size)
		}
	}
	for _, e := range marketCatalog {
		if inst, ok := env.Installed[e.Package]; ok {
			t.Logf("candidate %-28s installed %s", e.Package, inst.Version)
			continue
		}
		root, err := RootItemFromIndex(u, e.Package, "")
		if err != nil {
			t.Logf("candidate %-28s ROOT-ERR %v", e.Package, err)
			continue
		}
		plan, _, err := Solve(u, env, root)
		if err != nil {
			t.Logf("candidate %-28s SOLVE-ERR %v", e.Package, err)
			continue
		}
		bad, total := []string{}, int64(0)
		for _, p := range plan.Packages {
			if p.Installed {
				continue
			}
			total += p.InstalledSize
			if !isNoarch(p.Arch) {
				bad = append(bad, p.Name+"("+p.Arch+")")
			}
		}
		t.Logf("candidate %-28s pkgs=%d install=%dB non-noarch=%v", e.Package, len(plan.Packages), total, bad)
		for _, p := range plan.Packages {
			t.Logf("    %-32s %-18s arch=%-12s size=%d install=%d installed=%v", p.Name, p.Version, p.Arch, p.Size, p.InstalledSize, p.Installed)
		}
	}
}

// TestLiveMarketInstallRollback 在真机上完成一次真实安装、回读、卸载回滚与再次回读。
// 需要 PROLITE_LIVE=1 与 PROLITE_INSTALL=1 双重开关，避免误触发写入。
func TestLiveMarketInstallRollback(t *testing.T) {
	if os.Getenv("PROLITE_LIVE") != "1" || os.Getenv("PROLITE_INSTALL") != "1" {
		t.Skip("set PROLITE_LIVE=1 PROLITE_INSTALL=1 to run real install on the router")
	}
	marketID := os.Getenv("PROLITE_MARKET_ID")
	if marketID == "" {
		marketID = "uhttpd"
	}
	entry, ok := marketCatalogByID(marketID)
	if !ok {
		t.Fatalf("unknown market id %q", marketID)
	}
	r, _ := liveDial(t)
	a := &App{router: r, jobs: map[string]*InstallJob{}}
	ctx := context.Background()

	before, err := r.ReadPkgEnv()
	if err != nil {
		t.Fatalf("read env: %v", err)
	}
	if _, exists := before.Installed[entry.Package]; exists {
		t.Fatalf("%s 已安装，无法做干净安装验证", entry.Package)
	}

	id, err := a.StartInstallJob(InstallRequest{
		MarketID: marketID, DryRun: false, Commit: true,
		Confirmed: true, ConfirmWrites: true, ConfirmText: "192.168.100.1",
	})
	if err != nil {
		t.Fatalf("start install: %v", err)
	}
	view := waitJob(t, a, id, 10*time.Minute)
	for _, c := range view.Checks {
		t.Logf("check %-9s %-28s %-4s %s", c.Stage, c.ID, c.Level, c.Detail)
	}
	for _, f := range view.Files {
		t.Logf("file %-32s size=%d sent=%d status=%s", f.Name, f.Size, f.Sent, f.Status)
	}
	if view.Result == nil {
		t.Fatalf("no result; last log=%v", view.Log)
	}
	t.Logf("result success=%v code=%s message=%s", view.Result.Success, view.Result.Code, view.Result.Message)
	t.Logf("installed=%v changes=%+v", view.Result.Installed, view.Result.Changes)
	for _, line := range view.Log {
		t.Logf("log: %s", line)
	}
	if !view.Result.Success {
		t.Fatalf("install failed: %s %s", view.Result.Code, view.Result.Message)
	}

	// 装后回读
	afterInstall, err := r.ReadPkgEnv()
	if err != nil {
		t.Fatalf("read back after install: %v", err)
	}
	inst, exists := afterInstall.Installed[entry.Package]
	if !exists {
		t.Fatalf("回读未发现 %s", entry.Package)
	}
	t.Logf("回读确认：%s %s 已安装", entry.Package, inst.Version)

	// 卸载回滚，不使用 --force / --allow-untrusted
	cmd := "apk del " + shq(entry.Package)
	t.Logf("$ %s", cmd)
	out, err := r.execJob(ctx, cmd, 180*time.Second, func(s string) { t.Logf("del: %s", s) })
	if err != nil {
		t.Fatalf("uninstall exec: %v", err)
	}
	t.Logf("del exit=%d output=%s", out.ExitCode, strings.TrimSpace(out.Output))
	if out.ExitCode != 0 {
		t.Fatalf("uninstall failed exit=%d", out.ExitCode)
	}

	// 卸载后回读
	afterRemove, err := r.ReadPkgEnv()
	if err != nil {
		t.Fatalf("read back after uninstall: %v", err)
	}
	if _, still := afterRemove.Installed[entry.Package]; still {
		t.Fatalf("卸载后 %s 仍存在", entry.Package)
	}
	t.Logf("回读确认：%s 已卸载；包总数 %d → %d → %d",
		entry.Package, len(before.Installed), len(afterInstall.Installed), len(afterRemove.Installed))
	if len(afterRemove.Installed) != len(before.Installed) {
		t.Fatalf("包数量未恢复：安装前 %d，卸载后 %d", len(before.Installed), len(afterRemove.Installed))
	}

	// 远端临时目录应已清理
	tmpOut, _ := r.execJob(ctx, "ls -d /tmp/prolite-* 2>/dev/null", 15*time.Second, nil)
	if strings.TrimSpace(tmpOut.Output) != "" {
		t.Fatalf("远端临时目录未清理：%s", tmpOut.Output)
	}
	t.Logf("远端临时目录已清理")
}

func TestLiveMarketDryRunSQM(t *testing.T) {
	if os.Getenv("PROLITE_LIVE") != "1" {
		t.Skip("set PROLITE_LIVE=1 to run against the real router")
	}
	r, _ := liveDial(t)
	a := &App{router: r, jobs: map[string]*InstallJob{}}

	marketID := os.Getenv("PROLITE_MARKET_ID")
	if marketID == "" {
		marketID = "sqm"
	}
	id, err := a.StartInstallJob(InstallRequest{MarketID: marketID, DryRun: true})
	if err != nil {
		t.Fatalf("start dry-run: %v", err)
	}
	view := waitJob(t, a, id, 6*time.Minute)
	if view.Env != nil {
		t.Logf("env manager=%s arch=%s feeds=%d overlay=%dKB tmp=%dKB", view.Env.Manager, view.Env.Arch, len(view.Env.Feeds), view.Env.OverlayKB, view.Env.TmpKB)
	}
	for _, c := range view.Checks {
		t.Logf("check %-9s %-28s %-4s %s", c.Stage, c.ID, c.Level, c.Detail)
	}
	if view.Plan != nil {
		for _, p := range view.Plan.Packages {
			t.Logf("pkg %-24s %-14s arch=%-12s size=%d install=%d installed=%v", p.Name, p.Version, p.Arch, p.Size, p.InstalledSize, p.Installed)
		}
		t.Logf("plan totalDownload=%d totalInstall=%d warnings=%v", view.Plan.TotalSize, view.Plan.TotalInstall, view.Plan.Warnings)
	}
	for _, f := range view.Files {
		t.Logf("file %-32s size=%d sent=%d status=%s", f.Name, f.Size, f.Sent, f.Status)
	}
	if view.Result == nil {
		t.Fatalf("no result; last log: %v", view.Log)
	}
	t.Logf("result success=%v code=%s message=%s", view.Result.Success, view.Result.Code, view.Result.Message)
	if !view.Result.Success {
		for _, line := range view.Log {
			t.Logf("log: %s", line)
		}
		t.Fatalf("dry-run failed: %s %s", view.Result.Code, view.Result.Message)
	}
}
