package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestParseAPK3Cached 解析 apk-tools 3.x 的 ADB 包；缓存不存在时跳过。
func TestParseAPK3Cached(t *testing.T) {
	matches, _ := filepath.Glob(filepath.Join(os.Getenv("APPDATA"), "ProLite", "cache", "packages", "*.apk"))
	if len(matches) == 0 {
		t.Skip("没有缓存的 apk v3 包")
	}
	meta, err := ParsePackage(matches[0])
	if err != nil {
		t.Fatalf("ParsePackage: %v", err)
	}
	t.Logf("name=%s version=%s arch=%s size=%d installed=%d deps=%v scripts=%v",
		meta.Name, meta.Version, meta.Arch, meta.Size, meta.InstalledSize, meta.Depends, meta.HasScripts)
	if meta.Name == "" || meta.Version == "" || meta.Arch == "" || meta.InstalledSize == 0 {
		t.Fatalf("apk v3 元数据不完整：%+v", meta)
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0", "1.0", 0},
		{"1.0", "1.1", -1},
		{"1.10", "1.9", 1},
		{"1.0-r2", "1.0-r1", 1},
		{"2:1.0", "1:9.9", 1},
		{"6.18.26~abc-r1", "6.18.26~abc-r1", 0},
	}
	for _, c := range cases {
		got := compareVersions(c.a, c.b)
		if got != c.want {
			t.Fatalf("compareVersions(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestDepNameOp(t *testing.T) {
	cases := []struct {
		in, name, op, ver string
	}{
		{"libc", "libc", "", ""},
		{"foo>=1.2", "foo", ">=", "1.2"},
		{"bar (<< 2.0)", "bar", "<<", "2.0"},
		{"baz~1.2", "baz", "~", "1.2"},
		{"!conflict", "conflict", "", ""},
	}
	for _, c := range cases {
		n, o, v := depNameOp(c.in)
		if n != c.name || o != c.op || v != c.ver {
			t.Fatalf("depNameOp(%q)=(%q,%q,%q) want (%q,%q,%q)", c.in, n, o, v, c.name, c.op, c.ver)
		}
	}
}

func TestSplitCommaListKeepsConstraints(t *testing.T) {
	got := splitCommaList("libc, libfoo (>= 1.2), kmod-x")
	if len(got) != 3 || got[1] != "libfoo (>= 1.2)" {
		t.Fatalf("unexpected split: %#v", got)
	}
}

func TestKernelCompatible(t *testing.T) {
	if !kernelCompatible("6.18.26~abc", "6.18.26~abc-r1", "6.18.26") {
		t.Fatal("expected compatible")
	}
	if kernelCompatible("6.18.24~abc", "6.18.26~def-r1", "6.18.26") {
		t.Fatal("expected incompatible")
	}
}

func TestSolveDependencyOrder(t *testing.T) {
	u := &Universe{
		ByName:    map[string][]*IndexPkg{},
		Providers: map[string][]*IndexPkg{},
		Installed: map[string]InstalledPkg{},
	}
	add := func(p *IndexPkg) { u.add(p) }
	add(&IndexPkg{Name: "app", Version: "1.0", Arch: "mipsel_24kc", Depends: []string{"libfoo>=1.0"}})
	add(&IndexPkg{Name: "libfoo", Version: "1.2", Arch: "mipsel_24kc", Depends: []string{"libc"}})
	u.finalize()

	env := &PkgEnv{Manager: "apk", Arch: "mipsel_24kc", Installed: map[string]InstalledPkg{
		"libc": {Name: "libc", Version: "1.0"},
	}}
	root := planItemFromIndex(u.ByName["app"][0])
	root.IsRoot = true
	plan, _, err := Solve(u, env, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Packages) != 2 {
		t.Fatalf("want 2 packages, got %d", len(plan.Packages))
	}
	if plan.Packages[0].Name != "libfoo" || plan.Packages[1].Name != "app" {
		t.Fatalf("bad order: %s then %s", plan.Packages[0].Name, plan.Packages[1].Name)
	}
}

func TestPreflightArchMismatch(t *testing.T) {
	plan := &InstallPlan{Packages: []*PlanItem{{Name: "x", Version: "1", Arch: "x86_64", Size: 1, InstalledSize: 1}}}
	checks := preflightPlan(plan, &PkgEnv{Arch: "mipsel_24kc", OverlayKB: 100000, TmpKB: 100000})
	found := false
	for _, c := range checks {
		if c.ID == "A-ARCH-x" && c.Level == "fail" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected arch failure")
	}
}
