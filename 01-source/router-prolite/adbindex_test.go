package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestADBIndexURL(t *testing.T) {
	cases := []struct {
		in     string
		index  string
		base   string
		hasADB bool
	}{
		{"https://example.com/snapshots/packages/mipsel_24kc/base", "https://example.com/snapshots/packages/mipsel_24kc/base/packages.adb", "https://example.com/snapshots/packages/mipsel_24kc/base", true},
		{"https://example.com/snapshots/packages/mipsel_24kc/base/packages.adb", "https://example.com/snapshots/packages/mipsel_24kc/base/packages.adb", "https://example.com/snapshots/packages/mipsel_24kc/base", true},
		{"https://example.com/base/APKINDEX.tar.gz", "", "https://example.com/base", false},
	}
	for _, c := range cases {
		idx, base, ok := adbIndexURL(c.in)
		if idx != c.index || base != c.base || ok != c.hasADB {
			t.Fatalf("adbIndexURL(%q) = (%q,%q,%v)，期望 (%q,%q,%v)", c.in, idx, base, ok, c.index, c.base, c.hasADB)
		}
	}
	if got := expandRepoVars("https://x/$arch/base", "mipsel_24kc"); got != "https://x/mipsel_24kc/base" {
		t.Fatalf("$arch 展开失败：%s", got)
	}
}

// TestParseADBIndexLive 读取本机 TEMP 下的真实 packages.adb（由只读拉取得到）。
// 文件不存在时跳过，保证其他环境可正常跑测试。
func TestParseADBIndexLive(t *testing.T) {
	path := filepath.Join(os.TempDir(), "packages.adb")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skip("未找到本地 packages.adb，跳过真机索引解析测试")
	}
	pkgs, note, err := parseADBIndex(raw, Feed{
		Name: "base",
		URL:  "https://downloads.immortalwrt.org/snapshots/packages/mipsel_24kc/base",
	})
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if len(pkgs) < 100 {
		t.Fatalf("包数量异常：%d", len(pkgs))
	}
	byName := map[string]*IndexPkg{}
	for _, p := range pkgs {
		byName[p.Name] = p
	}
	if p := byName["464xlat"]; p == nil {
		t.Fatal("未解析到 464xlat")
	} else {
		t.Logf("464xlat version=%s size=%d installed=%d arch=%s deps=%v checksum=%s",
			p.Version, p.Size, p.InstalledSize, p.Arch, p.Depends, p.Checksum)
	}
	for i, p := range pkgs {
		if i >= 5 {
			break
		}
		t.Logf("sample[%d] %s %s size=%d", i, p.Name, p.Version, p.Size)
	}
	t.Logf("解析包数量=%d；%s", len(pkgs), note)
}

// TestADBPackagePayloadHash 验证 ADB index 的 hashes 对应包内元数据块 payload，
// 而不是整个 .apk 文件的 SHA256。
func TestADBPackagePayloadHash(t *testing.T) {
	path := filepath.Join(os.TempDir(), "464xlat.apk")
	if _, err := os.Stat(path); err != nil {
		t.Skip("未找到本地 464xlat.apk，跳过")
	}
	sum, err := adbPackagePayloadSHA256(path)
	if err != nil {
		t.Fatalf("计算失败：%v", err)
	}
	const want = "9621500568100f99eca58eac5fe6b5d28cb5e20ac5b7668a913b7e4b6707b8ee"
	if sum != want {
		t.Fatalf("元数据块 SHA256 = %s，期望 %s", sum, want)
	}
}
