package main

import (
	"fmt"
	"sort"
	"strings"
)

type depSpec struct {
	name     string
	op       string
	ver      string
	negative bool
	virtual  bool
}

type depGroup struct {
	alts []depSpec
}

func depGroups(manager string, deps []string) []depGroup {
	out := []depGroup{}
	if manager == "opkg" {
		for _, d := range deps {
			for _, alt := range strings.Split(d, "|") {
				n, op, v := depNameOp(alt)
				if n == "" {
					continue
				}
				out = append(out, depGroup{alts: []depSpec{{name: n, op: op, ver: v}}})
			}
		}
		return out
	}
	for _, d := range deps {
		neg := strings.HasPrefix(strings.TrimSpace(d), "!")
		n, op, v := depNameOp(d)
		if n == "" {
			continue
		}
		if neg {
			out = append(out, depGroup{alts: []depSpec{{name: n, negative: true}}})
			continue
		}
		spec := depSpec{name: n, op: op, ver: v}
		if strings.Contains(n, ":") {
			spec.virtual = true
		}
		out = append(out, depGroup{alts: []depSpec{spec}})
	}
	return out
}

// Solve 求解依赖闭包并返回依赖在前的安装计划。
func Solve(u *Universe, env *PkgEnv, root *PlanItem) (*InstallPlan, []Check, error) {
	checks := []Check{}
	selected := map[string]*PlanItem{}
	var order []*PlanItem

	var need func(spec depSpec, stack []string) error
	need = func(spec depSpec, stack []string) error {
		if spec.negative {
			return nil
		}
		if spec.name == "kernel" {
			if !kernelCompatible(spec.ver, env.KernelPkg, env.Kernel) {
				return fmt.Errorf("内核对不上：包要求 kernel %s，设备为 %s", spec.ver, env.KernelPkg)
			}
			return nil
		}
		if inst, ok := env.Installed[spec.name]; ok && versionSatisfies(inst.Version, spec.op, spec.ver) {
			return nil
		}
		if p, ok := selected[spec.name]; ok {
			if versionSatisfies(p.Version, spec.op, spec.ver) {
				return nil
			}
			return fmt.Errorf("%s 已选择 %s，但依赖要求 %s %s", spec.name, p.Version, spec.op, spec.ver)
		}
		for _, s := range stack {
			if s == spec.name {
				return fmt.Errorf("依赖环：%s", strings.Join(append(stack, spec.name), " → "))
			}
		}

		cands := u.ByName[spec.name]
		via := ""
		if len(cands) == 0 {
			cands = u.Providers[spec.name]
			via = "（虚拟包）"
			// 设备上已安装的提供者优先视为满足：例如 ddns-scripts 依赖虚拟包 ip，
			// 设备已装 ip-full 时不能再拉 ip-tiny，否则两者冲突导致 simulate 失败。
			for _, c := range cands {
				if _, ok := env.Installed[c.Name]; ok {
					return nil
				}
			}
		}
		var pick *IndexPkg
		for _, c := range cands {
			if !archMatches(c.Arch, env.Arch) {
				continue
			}
			if !versionSatisfies(c.Version, spec.op, spec.ver) {
				continue
			}
			pick = c
			break
		}
		if pick == nil {
			if spec.virtual {
				return fmt.Errorf("虚拟依赖 %s 无法满足（没有提供者）", spec.name)
			}
			return fmt.Errorf("依赖 %s%s 无法满足（索引中没有匹配版本）", spec.name, constraintText(spec.op, spec.ver))
		}
		if prev, ok := selected[pick.Name]; ok {
			if versionSatisfies(prev.Version, spec.op, spec.ver) {
				return nil
			}
			return fmt.Errorf("%s 已选择 %s，但依赖要求 %s %s", pick.Name, prev.Version, spec.op, spec.ver)
		}
		item := planItemFromIndex(pick)
		item.Reason = depReason(stack, spec.name) + via
		selected[item.Name] = item
		order = append(order, item)
		for _, g := range depGroups(env.Manager, item.Depends) {
			var lastErr error
			ok := false
			for _, alt := range g.alts {
				if alt.negative {
					continue
				}
				if err := need(alt, append(stack, item.Name)); err == nil {
					ok = true
					break
				} else {
					lastErr = err
				}
			}
			if !ok && lastErr != nil {
				return lastErr
			}
		}
		return nil
	}

	if root == nil {
		return nil, checks, fmt.Errorf("缺少目标包")
	}
	if inst, ok := env.Installed[root.Name]; ok {
		if root.Version == "" || compareVersions(inst.Version, root.Version) == 0 {
			root.Installed = true
			root.Status = "installed"
			root.Reason = "设备已安装相同版本，空转跳过"
			detail := fmt.Sprintf("%s 已安装 %s，计划将跳过写入", root.Name, inst.Version)
			checks = append(checks, Check{ID: "A-ALREADY", Stage: "A", Label: "空转保护", Level: "ok", Detail: detail})
			return &InstallPlan{
				Packages: []*PlanItem{root},
				Warnings: []string{detail},
			}, checks, nil
		}
		if compareVersions(inst.Version, root.Version) > 0 {
			return nil, checks, fmt.Errorf("已安装更高版本 %s（目标 %s），默认不做降级", inst.Version, root.Version)
		}
	}

	selected[root.Name] = root
	order = append(order, root)
	for _, g := range depGroups(env.Manager, root.Depends) {
		var lastErr error
		ok := false
		for _, alt := range g.alts {
			if alt.negative {
				continue
			}
			if err := need(alt, []string{root.Name}); err == nil {
				ok = true
				break
			} else {
				lastErr = err
			}
		}
		if !ok && lastErr != nil {
			return nil, checks, lastErr
		}
	}

	items, err := topoSort(selected, env.Manager)
	if err != nil {
		return nil, checks, err
	}

	plan := &InstallPlan{Packages: items}
	for _, it := range items {
		plan.TotalSize += it.Size
		plan.TotalInstall += it.InstalledSize
	}
	plan.Warnings = append(plan.Warnings, checkPlanConflicts(items, env)...)
	checks = append(checks, Check{
		ID: "A-DEPS", Stage: "A", Label: "依赖闭包",
		Level: "ok", Detail: fmt.Sprintf("共 %d 个包（含目标包），总大小 %s", len(items), humanBytes(plan.TotalSize)),
	})
	return plan, checks, nil
}

func planItemFromIndex(p *IndexPkg) *PlanItem {
	return &PlanItem{
		Name: p.Name, Version: p.Version, Arch: p.Arch,
		URL: p.URL, Filename: p.Filename, Size: p.Size, InstalledSize: p.InstalledSize,
		Description: p.Description, License: p.License,
		Checksum: p.Checksum, ChecksumKind: p.ChecksumKind,
		Depends: p.Depends, Provides: p.Provides,
		Conflicts: p.Conflicts, Replaces: p.Replaces, Feed: p.Feed,
		Status: "pending",
	}
}

// RootItemFromIndex 按包名（可选版本）从索引取根包。
func RootItemFromIndex(u *Universe, name, version string) (*PlanItem, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("请输入包名")
	}
	list := u.ByName[name]
	var pick *IndexPkg
	for _, c := range list {
		if version != "" && c.Version != version {
			continue
		}
		if !archMatches(c.Arch, u.Env.Arch) {
			continue
		}
		pick = c
		break
	}
	if pick == nil {
		return nil, fmt.Errorf("索引中找不到 %s%s", name, func() string {
			if version == "" {
				return ""
			}
			return " " + version
		}())
	}
	item := planItemFromIndex(pick)
	item.IsRoot = true
	item.Reason = "目标包"
	return item, nil
}

// RootItemFromLocal 用本地包文件作为根包。
func RootItemFromLocal(meta *PkgMeta) *PlanItem {
	return &PlanItem{
		Name: meta.Name, Version: meta.Version, Arch: meta.Arch,
		LocalPath: meta.Path, Size: meta.Size, InstalledSize: meta.InstalledSize,
		SHA256: meta.SHA256, Depends: meta.Depends, Provides: meta.Provides,
		Conflicts: meta.Conflicts, Replaces: meta.Replaces,
		IsRoot: true, Reason: "本地文件", Status: "pending",
	}
}

func topoSort(selected map[string]*PlanItem, manager string) ([]*PlanItem, error) {
	state := map[string]int{} // 0 未访问 1 访问中 2 完成
	var out []*PlanItem
	var visit func(name string, stack []string) error
	visit = func(name string, stack []string) error {
		it, ok := selected[name]
		if !ok {
			return nil
		}
		switch state[name] {
		case 1:
			return fmt.Errorf("依赖环：%s", strings.Join(append(stack, name), " → "))
		case 2:
			return nil
		}
		state[name] = 1
		for _, g := range depGroups(manager, it.Depends) {
			for _, alt := range g.alts {
				if alt.negative {
					continue
				}
				if _, ok := selected[alt.name]; ok {
					if err := visit(alt.name, append(stack, name)); err != nil {
						return err
					}
				}
			}
		}
		state[name] = 2
		out = append(out, it)
		return nil
	}
	names := make([]string, 0, len(selected))
	for n := range selected {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if err := visit(n, nil); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func checkPlanConflicts(items []*PlanItem, env *PkgEnv) []string {
	warnings := []string{}
	names := map[string]string{}
	for _, it := range items {
		names[it.Name] = it.Version
	}
	for _, it := range items {
		for _, c := range it.Conflicts {
			n, _, _ := depNameOp(strings.TrimPrefix(c, "!"))
			if _, ok := names[n]; ok {
				warnings = append(warnings, fmt.Sprintf("%s 与 %s 声明冲突", it.Name, n))
			}
			if _, ok := env.Installed[n]; ok {
				warnings = append(warnings, fmt.Sprintf("%s 与已安装的 %s 声明冲突", it.Name, n))
			}
		}
		for _, r := range it.Replaces {
			n, _, _ := depNameOp(r)
			if _, ok := env.Installed[n]; ok {
				warnings = append(warnings, fmt.Sprintf("%s 将替换已安装的 %s", it.Name, n))
			}
		}
	}
	return warnings
}

func kernelCompatible(want, havePkg, uname string) bool {
	if want == "" {
		return true
	}
	have := havePkg
	if have == "" {
		have = uname
	}
	if have == "" {
		return true
	}
	want = strings.TrimSpace(want)
	have = strings.TrimSpace(have)
	if want == have {
		return true
	}
	// 设备 kernel 包版本常带 -rN 修订后缀
	if i := strings.LastIndex(have, "-r"); i > 0 {
		have = have[:i]
	}
	if want == have || strings.HasPrefix(have, want+"-") || strings.HasPrefix(want, have+"-") {
		return true
	}
	// 只比较主版本 + ABI 前缀
	wb, _, _ := strings.Cut(want, "~")
	hb, _, _ := strings.Cut(have, "~")
	return wb == hb && strings.Contains(want, "~") && strings.Contains(have, "~")
}

func constraintText(op, ver string) string {
	if op == "" || ver == "" {
		return ""
	}
	return " (" + op + " " + ver + ")"
}

func depReason(stack []string, name string) string {
	if len(stack) == 0 {
		return "依赖 " + name
	}
	return "由 " + stack[len(stack)-1] + " 依赖"
}

func humanBytes(n int64) string {
	if n <= 0 {
		return "未知"
	}
	switch {
	case n >= 1024*1024*1024:
		return fmt.Sprintf("%.1f GB", float64(n)/1073741824)
	case n >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/1048576)
	case n >= 1024:
		return fmt.Sprintf("%.0f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%d B", n)
}
