package main

import "testing"

// 指纹按 host:port 分别保存：管理第二台设备时不能复用第一台的指纹。
func TestFingerprintPerDevice(t *testing.T) {
	cfg := savedConfig{Host: "192.168.100.1", Port: 22, Fingerprints: map[string]string{}}
	cfg.setFingerprint("192.168.100.1", 22, "SHA256:aaa")
	cfg.setFingerprint("10.0.0.1", 22, "SHA256:bbb")

	if got := cfg.fingerprintFor("192.168.100.1", 22); got != "SHA256:aaa" {
		t.Fatalf("device A fingerprint = %q, want SHA256:aaa", got)
	}
	if got := cfg.fingerprintFor("10.0.0.1", 22); got != "SHA256:bbb" {
		t.Fatalf("device B fingerprint = %q, want SHA256:bbb", got)
	}
	if got := cfg.fingerprintFor("10.0.0.2", 22); got != "" {
		t.Fatalf("unknown device should have no pinned fingerprint, got %q", got)
	}
	// 同一地址不同端口视为不同设备
	if got := cfg.fingerprintFor("192.168.100.1", 2222); got != "" {
		t.Fatalf("different port should not reuse fingerprint, got %q", got)
	}
}

// 旧版单值指纹只对配置里保存的主机生效，不能泄漏给其它设备。
func TestFingerprintLegacyMigration(t *testing.T) {
	cfg := savedConfig{Host: "192.168.100.1", Port: 22, Fingerprint: "SHA256:legacy"}
	if got := cfg.fingerprintFor("192.168.100.1", 22); got != "SHA256:legacy" {
		t.Fatalf("legacy fingerprint for saved host = %q, want SHA256:legacy", got)
	}
	if got := cfg.fingerprintFor("10.0.0.1", 22); got != "" {
		t.Fatalf("legacy fingerprint leaked to another host: %q", got)
	}
	cfg.setFingerprint("192.168.100.1", 22, "SHA256:new")
	cfg.Fingerprint = ""
	if got := cfg.fingerprintFor("192.168.100.1", 22); got != "SHA256:new" {
		t.Fatalf("migrated fingerprint = %q, want SHA256:new", got)
	}
}

// 设备侧解析要从根包名与 simulate 输出里识别系统组件，供二次确认使用。
func TestDeviceDangerNames(t *testing.T) {
	if got := deviceDangerNames("luci-app-uhttpd", ""); len(got) != 0 {
		t.Fatalf("safe package flagged: %v", got)
	}
	got := deviceDangerNames("some-plugin",
		"(1/3) Installing kmod-usb-net-rtl8152 (2.17.0)\n(2/3) Installing some-plugin (1.0)\nOK")
	if len(got) != 1 || got[0] != "kmod-usb-net-rtl8152" {
		t.Fatalf("simulate output danger detection = %v", got)
	}
	got = deviceDangerNames("kmod-foo", "")
	if len(got) != 1 || got[0] != "kmod-foo" {
		t.Fatalf("root package danger detection = %v", got)
	}
}

// 版本约束：>> 是严格大于，<< 是严格小于（Debian/opkg 语义）。
func TestVersionSatisfiesStrictOps(t *testing.T) {
	if versionSatisfies("1.0", ">>", "1.0") {
		t.Fatal(">> must be strict greater-than")
	}
	if !versionSatisfies("1.1", ">>", "1.0") {
		t.Fatal("1.1 >> 1.0 should hold")
	}
	if versionSatisfies("1.0", "<<", "1.0") {
		t.Fatal("<< must be strict less-than")
	}
	if !versionSatisfies("1.0", "<=", "1.0") {
		t.Fatal("<= must accept equality")
	}
}
