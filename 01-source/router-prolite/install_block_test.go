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
