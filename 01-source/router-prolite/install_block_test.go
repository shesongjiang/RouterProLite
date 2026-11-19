package main

import "testing"

func TestDangerousPackagesHardBlocked(t *testing.T) {
	cases := []string{"libc", "base-files", "kernel", "kmod-tun", "apk", "opkg", "uboot-envtools", "linux-6.6"}
	for _, name := range cases {
		if !dangerousPackage(name) {
			t.Fatalf("%s 应被判定为危险包", name)
		}
	}

	plan := &InstallPlan{Packages: []*PlanItem{{
		Name: "libc", Version: "1.0", Arch: "mipsel_24kc", Size: 1024,
	}}}
	checks := preflightPlan(plan, &PkgEnv{Manager: "apk", Arch: "mipsel_24kc"})
	found := false
	for _, c := range checks {
		if c.ID == "DANGER_FORBIDDEN" && c.Level == "fail" {
			found = true
		}
	}
	if !found {
		t.Fatal("危险包必须产生 DANGER_FORBIDDEN fail，任何确认都不能绕过")
	}
	if !plan.Packages[0].Dangerous {
		t.Fatal("危险包计划项应标记 dangerous=true")
	}
}
