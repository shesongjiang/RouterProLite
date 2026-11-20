package main

import (
	"strings"
	"testing"
)

func TestDangerousPackagesRequireConfirm(t *testing.T) {
	cases := []string{"libc", "base-files", "kernel", "kmod-tun", "apk", "opkg", "uboot-envtools", "linux-6.6"}
	for _, name := range cases {
		if !dangerousPackage(name) {
			t.Fatalf("%s 应被判定为系统组件", name)
		}
	}

	plan := &InstallPlan{Packages: []*PlanItem{{
		Name: "libc", Version: "1.0", Arch: "mipsel_24kc", Size: 1024,
	}}}
	checks := preflightPlan(plan, &PkgEnv{Manager: "apk", Arch: "mipsel_24kc"})
	found := false
	for _, c := range checks {
		if strings.HasPrefix(c.ID, "DANGER-REVIEW-") && c.Level == "warn" {
			found = true
		}
		if c.Level == "fail" && strings.HasPrefix(c.ID, "DANGER") {
			t.Fatalf("系统组件不应再被硬性拒绝：%s %s", c.ID, c.Detail)
		}
	}
	if !found {
		t.Fatal("系统组件必须产生 warn 提示，要求二次确认")
	}
	if !plan.Packages[0].Dangerous {
		t.Fatal("系统组件计划项应标记 dangerous=true")
	}
}

func TestPCModeRejectsKernelPackages(t *testing.T) {
	plan := &InstallPlan{Packages: []*PlanItem{
		{Name: "luci-app-sqm", Version: "1", Arch: "all"},
		{Name: "sqm-scripts", Version: "1", Arch: "all"},
		{Name: "kmod-ifb", Version: "1", Arch: "mipsel_24kc"},
	}}
	blocked := pcModeBlocked(plan)
	if len(blocked) != 1 || blocked[0] != "kmod-ifb" {
		t.Fatalf("kmod 必须被本机闭包拒绝：%v", blocked)
	}
	if !strings.Contains(pcModeBlockedMessage(blocked), "内核哈希") {
		t.Fatal("拒绝说明必须写明内核哈希原因")
	}
	if !kernelRelatedItem(&PlanItem{Name: "some-tool", Depends: []string{"kernel (= 6.6.1)"}}) {
		t.Fatal("依赖 kernel 的包也应被视为内核相关")
	}
	okPlan := &InstallPlan{Packages: []*PlanItem{{Name: "luci-app-ddns", Version: "1", Arch: "all"}}}
	if got := pcModeBlocked(okPlan); len(got) != 0 {
		t.Fatalf("纯脚本包不应被拒绝：%v", got)
	}
}

func TestClassifyFailureCategories(t *testing.T) {
	cases := []struct {
		code string
		msg  string
		want string
	}{
		{"IDX_UNREACHABLE", "dial tcp 1.2.3.4:443: connect: connection refused", "源不可达"},
		{"DEP_UNRESOLVED", "依赖 kmod-tun 无法满足", "依赖缺失"},
		{"SIMULATE_FAILED", "ERROR: UNTRUSTED signature", "签名不受信"},
		{"ARCH_MISMATCH", "包架构 x86_64，设备 mipsel_24kc", "架构或内核哈希不匹配"},
		{"KERNEL_MISMATCH", "包要求 kernel 6.6.1，设备 5.15.0", "架构或内核哈希不匹配"},
		{"SPACE_OVERLAY", "overlay 空间不足", "空间不足"},
		{"INSTALL_FAILED", "apk add: no space left on device", "空间不足"},
	}
	for _, c := range cases {
		got, hint := classifyFailure(c.code, c.msg)
		if got != c.want {
			t.Fatalf("%s/%s 分类为 %s，期望 %s", c.code, c.msg, got, c.want)
		}
		if strings.TrimSpace(hint) == "" {
			t.Fatalf("%s 缺少中文建议", c.code)
		}
	}
}

func TestPrepareInstallRequestDefaultsToDevice(t *testing.T) {
	req := InstallRequest{Mode: "feed", Package: "luci-app-ddns", DryRun: true}
	if err := prepareInstallRequest(&req); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if req.ResolveMode != "device" {
		t.Fatalf("默认解析方式应为 device，实际 %s", req.ResolveMode)
	}
	bad := InstallRequest{Mode: "feed", Package: "x", DryRun: true, ResolveMode: "cloud"}
	if err := prepareInstallRequest(&bad); err == nil {
		t.Fatal("非法解析方式应报错")
	}
}
