package main

import (
	"strings"
	"testing"
)

func TestPluginCatalogIntegrity(t *testing.T) {
	ids := map[string]bool{}
	pkgs := map[string]bool{}
	for _, e := range marketCatalog {
		if e.ID == "" || e.Package == "" || e.Title == "" || e.Description == "" || e.Category == "" {
			t.Fatalf("catalog entry missing fields: %+v", e)
		}
		if ids[e.ID] {
			t.Fatalf("duplicate id: %s", e.ID)
		}
		if pkgs[e.Package] {
			t.Fatalf("duplicate package: %s", e.Package)
		}
		if !strings.HasPrefix(e.Package, "luci-app-") {
			t.Fatalf("market only allows LuCI/script apps, got %s", e.Package)
		}
		if dangerousPackage(e.Package) || strings.HasPrefix(e.Package, "kmod-") {
			t.Fatalf("dangerous package in catalog: %s", e.Package)
		}
		ids[e.ID] = true
		pkgs[e.Package] = true
	}
	if _, ok := marketCatalogByID("sqm"); !ok {
		t.Fatal("catalog lookup failed")
	}
	if _, ok := marketCatalogByID("missing"); ok {
		t.Fatal("unknown catalog id should fail")
	}
}

func TestIsNoarch(t *testing.T) {
	for _, arch := range []string{"", "all", "noarch", "NoArch", "any", "*", " all "} {
		if !isNoarch(arch) {
			t.Fatalf("%q should be noarch", arch)
		}
	}
	for _, arch := range []string{"mipsel_24kc", "aarch64_cortex-a53", "x86_64"} {
		if isNoarch(arch) {
			t.Fatalf("%q should not be noarch", arch)
		}
	}
}

func TestMarketPlanChecksRejectsCompiledAndOversize(t *testing.T) {
	plan := &InstallPlan{Packages: []*PlanItem{
		{Name: "luci-app-sqm", Arch: "all", Size: 1000, InstalledSize: 2000},
		{Name: "compiled-dep", Arch: "mipsel_24kc", Size: 1000, InstalledSize: 2000},
		{Name: "big-pkg", Arch: "noarch", Size: 1000, InstalledSize: marketMaxPkgInstall + 1},
	}}
	checks := marketPlanChecks(plan)
	if !hasCheck(checks, "MARKET-ARCH-compiled-dep") {
		t.Fatalf("compiled dependency not rejected: %+v", checks)
	}
	if !hasCheck(checks, "MARKET-SIZE-big-pkg") {
		t.Fatalf("oversize package not rejected: %+v", checks)
	}
}

func TestMarketPlanChecksTotalLimitAndInstalledSkip(t *testing.T) {
	per := marketMaxTotalInstall/4 + 1
	plan := &InstallPlan{Packages: []*PlanItem{
		{Name: "a", Arch: "noarch", InstalledSize: per},
		{Name: "b", Arch: "noarch", InstalledSize: per},
		{Name: "c", Arch: "noarch", InstalledSize: per},
		{Name: "d", Arch: "noarch", InstalledSize: per},
		{Name: "already", Arch: "mipsel_24kc", Installed: true, InstalledSize: marketMaxPkgInstall * 10},
	}}
	checks := marketPlanChecks(plan)
	if !hasCheck(checks, "MARKET-SIZE-TOTAL") {
		t.Fatalf("total limit not enforced: %+v", checks)
	}
	for _, c := range checks {
		if strings.Contains(c.ID, "already") {
			t.Fatalf("installed package should be skipped: %+v", c)
		}
	}
}

func TestMarketPlanChecksOK(t *testing.T) {
	plan := &InstallPlan{Packages: []*PlanItem{
		{Name: "luci-app-commands", Arch: "all", Size: 1024, InstalledSize: 2048},
	}}
	checks := marketPlanChecks(plan)
	if len(checks) != 1 || checks[0].Level != "ok" || checks[0].ID != "MARKET-LIMITS" {
		t.Fatalf("expected single ok check: %+v", checks)
	}
}

func TestPrepareInstallRequestMarket(t *testing.T) {
	req := InstallRequest{MarketID: "sqm", Mode: "local", Package: "evil", LocalPath: "/tmp/evil.ipk", DryRun: false, Commit: true, Repos: []string{"http://evil.example/feed"}}
	isMarket, err := prepareInstallRequest(&req)
	if err != nil || !isMarket {
		t.Fatalf("market request rejected: market=%v err=%v", isMarket, err)
	}
	if req.Mode != "feed" || req.Package != "luci-app-sqm" || req.LocalPath != "" || req.Version != "" || len(req.Repos) != 0 {
		t.Fatalf("market request not normalized: %+v", req)
	}

	if _, err := prepareInstallRequest(&InstallRequest{MarketID: "sqm", DryRun: false}); err == nil {
		t.Fatal("real install without commit should fail")
	}
	if _, err := prepareInstallRequest(&InstallRequest{MarketID: "missing", DryRun: true}); err == nil {
		t.Fatal("unknown market id should fail")
	}
}

func TestPrepareInstallRequestManualDryRunOnly(t *testing.T) {
	if _, err := prepareInstallRequest(&InstallRequest{Mode: "feed", Package: "x", DryRun: false, Commit: true}); err == nil {
		t.Fatal("manual real install must be rejected")
	}
	if _, err := prepareInstallRequest(&InstallRequest{Mode: "feed", Package: "x", DryRun: true}); err != nil {
		t.Fatalf("manual dry-run should pass: %v", err)
	}
	if _, err := prepareInstallRequest(&InstallRequest{Mode: "local", DryRun: true}); err == nil {
		t.Fatal("local without path should fail")
	}
}

func hasCheck(checks []Check, id string) bool {
	for _, c := range checks {
		if c.ID == id {
			return true
		}
	}
	return false
}
