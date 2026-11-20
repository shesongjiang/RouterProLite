package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// InstallJob 是一次预演/安装任务的内存状态。
type InstallJob struct {
	mu        sync.Mutex
	ID        string
	req       InstallRequest
	phase     string
	percent   int
	env       *PkgEnv
	plan      *InstallPlan
	checks    []Check
	files     []JobFile
	log       []string
	result    *InstallResult
	running   bool
	startedAt int64
	logPath   string
	cancel    context.CancelFunc
}

func (j *InstallJob) setPhase(phase string, pct int) {
	j.mu.Lock()
	j.phase = phase
	j.percent = pct
	j.mu.Unlock()
}

func (j *InstallJob) addLine(line string) {
	j.mu.Lock()
	path := j.logPath
	j.log = append(j.log, line)
	if len(j.log) > 400 {
		j.log = j.log[len(j.log)-400:]
	}
	j.mu.Unlock()
	if path != "" {
		appendInstallLog(path, line)
	}
}

func (j *InstallJob) addChecks(cs []Check) {
	j.mu.Lock()
	j.checks = append(j.checks, cs...)
	j.mu.Unlock()
}

func (j *InstallJob) setFile(name, status string, sent, total int64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for i := range j.files {
		if j.files[i].Name == name {
			j.files[i].Status = status
			if sent > 0 {
				j.files[i].Sent = sent
			}
			if total > 0 {
				j.files[i].Size = total
			}
			return
		}
	}
	j.files = append(j.files, JobFile{Name: name, Status: status, Sent: sent, Size: total})
}

func (j *InstallJob) finish(res *InstallResult) {
	if res != nil && !res.Success && res.Reason == "" {
		res.Reason, res.Hint = classifyFailure(res.Code, res.Message)
	}
	j.mu.Lock()
	j.result = res
	j.running = false
	if res != nil && res.Success {
		j.percent = 100
		j.phase = "done"
	} else {
		j.phase = "failed"
	}
	j.mu.Unlock()
}

func (j *InstallJob) view() *InstallJobView {
	j.mu.Lock()
	defer j.mu.Unlock()
	files := append([]JobFile(nil), j.files...)
	checks := append([]Check(nil), j.checks...)
	logs := append([]string(nil), j.log...)
	var res *InstallResult
	if j.result != nil {
		r := *j.result
		res = &r
	}
	return &InstallJobView{
		ID: j.ID, Phase: j.phase, Percent: j.percent, DryRun: j.req.DryRun,
		ResolveMode: j.req.ResolveMode,
		Running:     j.running, Env: j.env, Plan: j.plan, Checks: checks, Files: files,
		Log: logs, Result: res, StartedAt: j.startedAt, LogPath: j.logPath,
	}
}

// GetPkgEnv 只读返回设备的包管理器 / 架构 / 内核 / 源。
func (a *App) GetPkgEnv() (*PkgEnv, error) {
	return a.router.ReadPkgEnv()
}

// InspectLocalPackage 解析本地 .ipk / .apk 元数据。
func (a *App) InspectLocalPackage(path string) (*PkgMeta, error) {
	return ParsePackage(strings.TrimSpace(path))
}

// PickPackageFile 选择本地包文件。
func (a *App) PickPackageFile() (string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择 OpenWrt 安装包",
		Filters: []runtime.FileFilter{
			{DisplayName: "OpenWrt 包 (*.ipk;*.apk)", Pattern: "*.ipk;*.apk"},
			{DisplayName: "所有文件", Pattern: "*"},
		},
	})
	if err != nil {
		return "", err
	}
	return path, nil
}

// prepareInstallRequest 归一化并校验安装请求。
// 全量包列表与本地包都允许真实安装，写入前的授权由 authorizeDeviceWrite 统一校验。
func prepareInstallRequest(req *InstallRequest) error {
	req.Mode = strings.TrimSpace(req.Mode)
	req.ResolveMode = strings.ToLower(strings.TrimSpace(req.ResolveMode))
	req.Package = strings.TrimSpace(req.Package)
	req.LocalPath = strings.TrimSpace(req.LocalPath)

	if req.Mode != "feed" && req.Mode != "local" {
		return fmt.Errorf("安装模式必须是 feed 或 local")
	}
	if req.ResolveMode == "" {
		req.ResolveMode = "device"
	}
	if req.ResolveMode != "device" && req.ResolveMode != "pc" {
		return fmt.Errorf("依赖解析方式必须是 device 或 pc")
	}
	if req.Mode == "feed" && req.Package == "" {
		return fmt.Errorf("请输入要安装或预演的包名")
	}
	if req.Mode == "local" && req.LocalPath == "" {
		return fmt.Errorf("请选择本地包文件")
	}
	if !req.DryRun && !req.Commit {
		return fmt.Errorf("真实安装必须设置 commit=true")
	}
	return nil
}

// StartInstallJob 启动安装任务：dry-run 只预演，commit=true 才会写入设备。
func (a *App) StartInstallJob(req InstallRequest) (string, error) {
	if err := prepareInstallRequest(&req); err != nil {
		return "", err
	}
	if req.Mode == "local" {
		if _, err := ParsePackage(req.LocalPath); err != nil {
			return "", err
		}
	}
	if err := a.router.sessionAvailable(); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	jobID := fmt.Sprintf("job-%d", time.Now().UnixNano())
	job := &InstallJob{
		ID:        jobID,
		req:       req,
		phase:     "prepare",
		percent:   2,
		running:   true,
		startedAt: time.Now().UnixMilli(),
		logPath:   installLogPath(jobID),
		cancel:    cancel,
	}
	job.addLine(fmt.Sprintf("任务 %s 开始：mode=%s resolve=%s dryRun=%v package=%s",
		jobID, req.Mode, req.ResolveMode, req.DryRun, req.Package))
	a.jobsMu.Lock()
	if a.jobs == nil {
		a.jobs = map[string]*InstallJob{}
	}
	for id, old := range a.jobs {
		if !old.view().Running {
			delete(a.jobs, id)
		}
	}
	a.jobs[job.ID] = job
	a.jobsMu.Unlock()

	go a.runInstallJob(ctx, job)
	return job.ID, nil
}

// CancelInstallJob 取消正在执行的预演任务。
func (a *App) CancelInstallJob(id string) error {
	a.jobsMu.Lock()
	job := a.jobs[id]
	a.jobsMu.Unlock()
	if job == nil {
		return fmt.Errorf("任务不存在")
	}
	job.cancel()
	job.addLine("已请求取消任务")
	return nil
}

// GetInstallJob 返回任务快照，供前端轮询。
func (a *App) GetInstallJob(id string) (*InstallJobView, error) {
	a.jobsMu.Lock()
	job := a.jobs[id]
	a.jobsMu.Unlock()
	if job == nil {
		return nil, fmt.Errorf("任务不存在")
	}
	return job.view(), nil
}

func (a *App) runInstallJob(ctx context.Context, job *InstallJob) {
	defer job.cancel()
	emit := func() {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "install:update", job.view())
		}
	}
	fail := func(code, msg string) {
		job.addLine("失败：" + msg)
		job.finish(&InstallResult{Code: code, Message: msg})
		emit()
	}
	done := func(sim string) {
		job.finish(&InstallResult{Success: true, Code: "DRY_RUN_OK", Message: "dry-run 通过，未写入设备", Simulate: sim})
		emit()
	}

	// 1. 设备包环境
	job.setPhase("env", 5)
	emit()
	env, err := a.router.ReadPkgEnv()
	if err != nil {
		fail("NET_READ_ENV", "读取设备包环境失败："+err.Error())
		return
	}
	job.mu.Lock()
	job.env = env
	job.mu.Unlock()
	job.addChecks([]Check{
		{ID: "ENV-PM", Stage: "index", Label: "包管理器", Level: levelOf(env.Manager != "", "ok", "fail"), Detail: orDash(env.Manager)},
		{ID: "ENV-ARCH", Stage: "index", Label: "架构", Level: levelOf(env.Arch != "", "ok", "fail"), Detail: orDash(env.Arch)},
		{ID: "ENV-KERNEL", Stage: "index", Label: "内核", Level: "ok", Detail: orDash(env.Kernel) + " / " + orDash(env.KernelPkg)},
		{ID: "ENV-FEEDS", Stage: "index", Label: "包源", Level: levelOf(len(env.Feeds) > 0, "ok", "warn"), Detail: fmt.Sprintf("%d 个", len(env.Feeds))},
	})
	if env.Manager == "" {
		fail("PKG_MGR_UNKNOWN", "设备未检测到 apk 或 opkg")
		return
	}
	if env.Arch == "" {
		fail("ARCH_UNKNOWN", "无法读取设备架构")
		return
	}
	if feeds := parseRepoFeedsForArch(job.req.Repos, env.Arch); len(feeds) > 0 {
		env.Feeds = feeds
		job.addChecks([]Check{{ID: "ENV-OVERRIDE", Stage: "index", Label: "自定义仓库",
			Level: "ok", Detail: fmt.Sprintf("使用手动填写的 %d 个仓库", len(feeds))}})
	}

	if job.req.ResolveMode == "device" {
		a.runDeviceSideJob(ctx, job, env, emit, fail, done)
		return
	}
	job.addChecks([]Check{{ID: "MODE-PC", Stage: "index", Label: "依赖解析方式",
		Level: "ok", Detail: "本机闭包后上传：PC 解析依赖树并下载全部包，逐个上传安装；内核相关包会被拒绝"}})

	// 2. 本地包直接解析；feed 模式需要索引
	var root *PlanItem
	var universe *Universe
	if job.req.Mode == "local" {
		meta, err := ParsePackage(job.req.LocalPath)
		if err != nil {
			fail("FORMAT_UNSUPPORTED", err.Error())
			return
		}
		expect := "apk"
		if env.Manager == "opkg" {
			expect = "ipk"
		}
		if meta.Format != expect {
			fail("PKG_MGR_MISMATCH", fmt.Sprintf("设备使用 %s，但文件是 %s", env.Manager, meta.Format))
			return
		}
		root = RootItemFromLocal(meta)
		job.addChecks([]Check{{ID: "A-FILE", Stage: "A", Label: "本地包", Level: "ok",
			Detail: fmt.Sprintf("%s %s（%s，%s）", meta.Name, meta.Version, meta.Arch, humanBytes(meta.Size))}})
		if len(meta.Depends) == 0 {
			job.setPhase("resolve", 25)
			plan := &InstallPlan{Packages: []*PlanItem{root}, TotalSize: root.Size, TotalInstall: root.InstalledSize, ResolveMode: "pc"}
			if blocked := pcModeBlocked(plan); len(blocked) > 0 {
				fail("PC_MODE_KERNEL_BLOCKED", pcModeBlockedMessage(blocked))
				return
			}
			job.mu.Lock()
			job.plan = plan
			job.mu.Unlock()
			initJobFiles(job, plan)
			a.runPreflightAndDryRun(ctx, job, env, plan, emit, fail, done)
			return
		}
	}

	job.setPhase("index", 15)
	emit()
	universe, idxChecks, err := a.router.FetchUniverse(env, job.addLine)
	job.addChecks(idxChecks)
	if err != nil {
		fail("IDX_UNREACHABLE", err.Error())
		return
	}

	job.setPhase("resolve", 25)
	emit()
	if job.req.Mode == "local" {
		// 用索引补全依赖，但根包仍使用本地文件
		if _, err := RootItemFromIndex(universe, root.Name, root.Version); err != nil {
			job.addLine("索引中没有同名包，依赖仍尝试从索引解析：" + err.Error())
		}
	} else {
		root, err = RootItemFromIndex(universe, job.req.Package, job.req.Version)
		if err != nil {
			fail("PKG_NOT_FOUND", err.Error())
			return
		}
	}
	plan, solveChecks, err := Solve(universe, env, root)
	job.addChecks(solveChecks)
	if err != nil {
		fail("DEP_UNRESOLVED", err.Error())
		return
	}
	plan.ResolveMode = "pc"
	if blocked := pcModeBlocked(plan); len(blocked) > 0 {
		fail("PC_MODE_KERNEL_BLOCKED", pcModeBlockedMessage(blocked))
		return
	}
	job.mu.Lock()
	job.plan = plan
	job.mu.Unlock()
	initJobFiles(job, plan)
	a.runPreflightAndDryRun(ctx, job, env, plan, emit, fail, done)
}

func (a *App) runPreflightAndDryRun(ctx context.Context, job *InstallJob, env *PkgEnv,
	plan *InstallPlan, emit func(), fail func(string, string), done func(string)) {

	// 3. 预检
	job.setPhase("preflight", 30)
	emit()
	checks := preflightPlan(plan, env)
	job.addChecks(checks)
	for _, c := range checks {
		if c.Level == "fail" {
			code := c.ID
			switch {
			case strings.Contains(c.ID, "OVERLAY"):
				code = "SPACE_OVERLAY"
			case strings.Contains(c.ID, "SPACE-TMP"):
				code = "SPACE_TMP"
			case strings.HasPrefix(c.ID, "A-ARCH-"):
				code = "ARCH_MISMATCH"
			case strings.HasPrefix(c.ID, "A-KERNEL-"):
				code = "KERNEL_MISMATCH"
			}
			fail(code, c.Label+"："+c.Detail)
			return
		}
	}

	// 4. 空转保护与真实写入授权
	needWork := false
	for _, it := range plan.Packages {
		if !it.Installed {
			needWork = true
			break
		}
	}
	if !needWork {
		job.addLine("所有计划项均已安装相同版本，空转保护：不下载、不写入设备")
		job.finish(&InstallResult{Success: true, Code: "ALREADY_INSTALLED",
			Message: "目标包与依赖均已安装相同版本，未写入设备"})
		emit()
		return
	}
	if !job.req.DryRun {
		if !job.req.Commit {
			fail("WRITE_NOT_AUTHORIZED", "真实安装必须设置 commit=true")
			return
		}
		hasDanger := false
		for _, it := range plan.Packages {
			if it.Dangerous && !it.Installed {
				hasDanger = true
				break
			}
		}
		if err := a.authorizeDeviceWrite(job.req, hasDanger); err != nil {
			fail("WRITE_NOT_AUTHORIZED", err.Error())
			return
		}
		job.addLine(fmt.Sprintf("设备写入授权校验通过：host=%s dangerous=%v", a.router.TargetHost(), hasDanger))
	}

	// 5. 下载
	job.setPhase("download", 35)
	emit()
	if err := DownloadPlan(ctx, plan, job.addLine, job.setFile); err != nil {
		if ctx.Err() != nil {
			fail("CANCELLED", "任务已取消，未写入设备")
		} else {
			fail("DL_FAILED", err.Error())
		}
		return
	}
	job.setPhase("verifyLocal", 55)
	emit()
	for _, it := range plan.Packages {
		if it.Installed {
			continue
		}
		path := it.Downloaded
		if path == "" {
			path = it.LocalPath
		}
		if path == "" {
			fail("DL_FAILED", it.Name+" 没有本地文件")
			return
		}
		meta, err := ParsePackage(path)
		if err != nil {
			fail("FORMAT_UNSUPPORTED", it.Name+"："+err.Error())
			return
		}
		if meta.Name != it.Name || meta.Version != it.Version {
			fail("DL_HASH", fmt.Sprintf("%s 元数据不一致（%s %s）", it.Name, meta.Name, meta.Version))
			return
		}
		if !archMatches(meta.Arch, env.Arch) {
			fail("ARCH_MISMATCH", fmt.Sprintf("%s 架构 %s，设备 %s", it.Name, meta.Arch, env.Arch))
			return
		}
		job.addChecks([]Check{{ID: "B-" + it.Name, Stage: "B", Label: it.Name,
			Level: "ok", Detail: fmt.Sprintf("元数据一致，%s", meta.Arch)}})
	}

	// 5. 上传
	job.setPhase("upload", 60)
	emit()
	defer a.router.cleanupRemoteDir(context.Background(), job.ID)
	remoteDir, err := a.router.uploadPlan(ctx, plan, job.ID, job.setFile, job.addLine)
	if err != nil {
		fail("TRANSFER_IO", err.Error())
		return
	}
	job.addChecks([]Check{{ID: "C-UPLOAD", Stage: "C", Label: "上传", Level: "ok", Detail: remoteDir}})

	// 6. dry-run simulate
	job.setPhase("simulate", 85)
	emit()
	simCmd, simOut, err := a.simulate(ctx, env, plan, job.addLine)
	job.addChecks([]Check{{ID: "C-SIM", Stage: "C", Label: "离线 simulate",
		Level: levelOf(err == nil, "ok", "fail"), Detail: firstNonEmpty(tailText(simOut, 300), orDash(errTextOf(err)))}})
	if err != nil {
		if ctx.Err() != nil {
			fail("CANCELLED", "任务已取消，未写入设备")
		} else {
			fail("INSTALL_FAILED", "simulate 失败："+err.Error())
		}
		return
	}
	if job.req.DryRun {
		done(simCmd)
		return
	}
	// 真实安装：写前快照 -> 逐包安装 -> 装后回读差异。
	job.setPhase("install", 90)
	emit()
	res := a.executeInstall(ctx, job, env, plan, simCmd, emit)
	job.finish(res)
	emit()
}

func (a *App) simulate(ctx context.Context, env *PkgEnv, plan *InstallPlan,
	logf func(string)) (string, string, error) {

	args := make([]string, 0, len(plan.Packages))
	for _, it := range plan.Packages {
		if it.Installed {
			continue
		}
		if it.RemotePath == "" {
			return "", "", fmt.Errorf("%s 缺少远端路径", it.Name)
		}
		args = append(args, shq(it.RemotePath))
	}
	files := strings.Join(args, " ")
	var cmd string
	if env.Manager == "opkg" {
		cmd = "opkg install --noaction " + files
	} else {
		// apk-tools 3.x 的主路径：空仓库文件 + simulate；--no-network 仅用于旧版回退。
		cmd = "apk --repositories-file /dev/null add --simulate " + files
	}
	logf("$ " + cmd)
	out, err := a.router.execJob(ctx, cmd, 180*time.Second, logf)
	if err != nil {
		return cmd, out.Output, err
	}
	if out.ExitCode == 0 {
		return cmd, out.Output, nil
	}
	// 旧版 apk 可能只支持 --no-network。
	if env.Manager == "apk" && (strings.Contains(out.Output, "unrecognized option") ||
		strings.Contains(out.Output, "unknown option") || strings.Contains(out.Output, "Usage")) {
		alt := "apk add --simulate --no-network " + files
		logf("当前 apk 不支持 --repositories-file，回退旧版 --no-network 分支")
		logf("$ " + alt)
		out2, err2 := a.router.execJob(ctx, alt, 180*time.Second, logf)
		if err2 != nil {
			return alt, out2.Output, err2
		}
		if out2.ExitCode == 0 {
			return alt, out2.Output, nil
		}
		return alt, out2.Output, fmt.Errorf("simulate 退出码 %d：%s", out2.ExitCode, tailText(out2.Output, 200))
	}
	return cmd, out.Output, fmt.Errorf("simulate 退出码 %d：%s", out.ExitCode, tailText(out.Output, 200))
}

func preflightPlan(plan *InstallPlan, env *PkgEnv) []Check {
	checks := []Check{}
	needInstall := int64(0)
	for _, it := range plan.Packages {
		if it.Installed {
			continue
		}
		needInstall += it.InstalledSize
		if !archMatches(it.Arch, env.Arch) {
			checks = append(checks, Check{ID: "A-ARCH-" + it.Name, Stage: "A", Label: "架构 " + it.Name,
				Level: "fail", Detail: fmt.Sprintf("包 %s，设备 %s", orDash(it.Arch), orDash(env.Arch))})
		}
		isKmod := strings.HasPrefix(it.Name, "kmod-")
		wantKernel := ""
		for _, d := range it.Depends {
			n, op, v := depNameOp(d)
			if n == "kernel" {
				isKmod = true
				wantKernel = v
				_ = op
			}
		}
		if isKmod && wantKernel != "" && !kernelCompatible(wantKernel, env.KernelPkg, env.Kernel) {
			checks = append(checks, Check{ID: "A-KERNEL-" + it.Name, Stage: "A", Label: "内核 ABI " + it.Name,
				Level: "fail", Detail: fmt.Sprintf("包要求 %s，设备 %s", wantKernel, orDash(env.KernelPkg))})
		}
		if dangerousPackage(it.Name) {
			it.Dangerous = true
			checks = append(checks, Check{ID: "DANGER-REVIEW-" + it.Name, Stage: "A", Label: "系统组件 " + it.Name,
				Level: "warn", Detail: "属于 kmod/内核/引导/base-files/libc/apk/opkg 等系统组件；安装后可能导致设备无法启动，需单独二次确认"})
		}
	}
	need := needInstall + needInstall/8
	if env.OverlayKB > 0 {
		avail := env.OverlayKB * 1024
		lv := "ok"
		detail := fmt.Sprintf("需要约 %s，可用 %s", humanBytes(need), humanBytes(avail))
		if need > avail {
			lv = "fail"
			detail += "；overlay 空间不足"
		}
		checks = append(checks, Check{ID: "A-SPACE-OVERLAY", Stage: "A", Label: "overlay 空间", Level: lv, Detail: detail})
	} else {
		checks = append(checks, Check{ID: "A-SPACE-OVERLAY", Stage: "A", Label: "overlay 空间", Level: "warn", Detail: "未能读取 overlay 可用空间"})
	}
	needTmp := plan.TotalSize + plan.TotalSize/10
	if env.TmpKB > 0 {
		avail := env.TmpKB * 1024
		lv := "ok"
		detail := fmt.Sprintf("上传约 %s，/tmp 可用 %s", humanBytes(needTmp), humanBytes(avail))
		if needTmp > avail {
			lv = "fail"
			detail += "；/tmp 空间不足"
		}
		checks = append(checks, Check{ID: "A-SPACE-TMP", Stage: "A", Label: "/tmp 空间", Level: lv, Detail: detail})
	}
	for _, w := range plan.Warnings {
		checks = append(checks, Check{ID: "A-CONFLICT", Stage: "A", Label: "依赖/冲突提示", Level: "warn", Detail: w})
	}
	checks = append(checks, Check{ID: "A-SUMMARY", Stage: "A", Label: "预检完成", Level: "ok",
		Detail: fmt.Sprintf("%d 个包，安装后约 %s", len(plan.Packages), humanBytes(needInstall))})
	return checks
}

func dangerousPackage(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	switch n {
	case "kernel", "base-files", "libc", "busybox", "dropbear", "procd", "netifd",
		"firewall4", "fstools", "mtd", "uboot-envtools", "apk", "opkg", "libc-dev", "kmod":
		return true
	}
	return strings.HasPrefix(n, "kmod-") || strings.HasPrefix(n, "uboot") ||
		strings.HasPrefix(n, "linux-") || strings.HasPrefix(n, "libc-")
}

func initJobFiles(job *InstallJob, plan *InstallPlan) {
	job.mu.Lock()
	defer job.mu.Unlock()
	job.files = make([]JobFile, 0, len(plan.Packages))
	for _, it := range plan.Packages {
		job.files = append(job.files, JobFile{Name: it.Name, Status: "pending", Size: it.Size})
	}
}

func levelOf(ok bool, good, bad string) string {
	if ok {
		return good
	}
	return bad
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func tailText(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func errTextOf(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// runDeviceSideJob 走设备侧解析：把包名或本地文件交给设备的 apk/opkg，
// 由设备自己解析依赖、按本机内核哈希选择 kmod、下载并安装。
// 预演使用设备上的 simulate；真实写入仍然经过授权、快照与装后回读。
func (a *App) runDeviceSideJob(ctx context.Context, job *InstallJob, env *PkgEnv,
	emit func(), fail func(string, string), done func(string)) {

	req := job.req
	job.addChecks([]Check{{ID: "MODE-DEVICE", Stage: "index", Label: "依赖解析方式",
		Level: "ok", Detail: "设备侧解析（默认）：由设备的 " + env.Manager + " 解析依赖、按内核哈希选择 kmod 并下载安装"}})

	root := &PlanItem{Name: req.Package, Version: req.Version, IsRoot: true, Status: "pending", Reason: "目标包（设备侧解析）"}
	target := req.Package

	if req.Mode == "local" {
		meta, err := ParsePackage(req.LocalPath)
		if err != nil {
			fail("FORMAT_UNSUPPORTED", err.Error())
			return
		}
		expect := "apk"
		if env.Manager == "opkg" {
			expect = "ipk"
		}
		if meta.Format != expect {
			fail("PKG_MGR_MISMATCH", fmt.Sprintf("设备使用 %s，但文件是 %s", env.Manager, meta.Format))
			return
		}
		root = RootItemFromLocal(meta)
		root.Reason = "本地文件（设备侧解析依赖）"
		plan := &InstallPlan{Packages: []*PlanItem{root}, ResolveMode: "device",
			TotalSize: root.Size, TotalInstall: root.InstalledSize}
		job.mu.Lock()
		job.plan = plan
		job.mu.Unlock()
		initJobFiles(job, plan)
		job.setPhase("upload", 30)
		emit()
		if _, err := a.router.uploadPlan(ctx, plan, job.ID, job.setFile, job.addLine); err != nil {
			fail("TRANSFER_IO", "上传本地包失败："+err.Error())
			return
		}
		defer a.router.cleanupRemoteDir(context.Background(), job.ID)
		if root.RemotePath == "" {
			fail("TRANSFER_IO", "本地包上传后没有远端路径")
			return
		}
		target = root.RemotePath
	} else {
		if inst, ok := env.Installed[root.Name]; ok {
			root.Installed = true
			root.Status = "installed"
			if strings.TrimSpace(root.Version) == "" {
				root.Version = inst.Version
			}
		}
		if req.Version != "" && env.Manager == "apk" {
			target = req.Package + "=" + req.Version
		}
		plan := &InstallPlan{Packages: []*PlanItem{root}, ResolveMode: "device"}
		job.mu.Lock()
		job.plan = plan
		job.mu.Unlock()
		initJobFiles(job, plan)
	}

	// 设备侧预演：apk add --simulate / opkg install --noaction
	job.setPhase("simulate", 45)
	emit()
	simCmd, simOut, simErr := a.deviceSimulate(ctx, env, target, job.addLine)
	job.addChecks([]Check{{ID: "DEV-SIM", Stage: "C", Label: "设备侧预演",
		Level:  levelOf(simErr == nil, "ok", "fail"),
		Detail: firstNonEmpty(tailText(simOut, 400), orDash(errTextOf(simErr)))}})
	if simErr != nil {
		if ctx.Err() != nil {
			fail("CANCELLED", "任务已取消，未写入设备")
		} else {
			fail("SIMULATE_FAILED", "设备侧预演失败："+simErr.Error())
		}
		return
	}
	job.addLine("设备侧预演通过：" + simCmd)
	if req.DryRun {
		done(simCmd + "\n" + simOut)
		return
	}
	if root.Installed && (req.Version == "" || req.Version == root.Version) {
		job.finish(&InstallResult{Success: true, Code: "ALREADY_INSTALLED",
			Message: "目标包已安装相同版本，未写入设备", Simulate: simCmd + "\n" + simOut})
		emit()
		return
	}

	// 系统组件识别：根包名 + simulate 输出里出现的 kmod/内核包。
	hasDanger := dangerousPackage(root.Name)
	dangerHits := dangerousNamesInText(simOut)
	for _, n := range dangerHits {
		if dangerousPackage(n) {
			hasDanger = true
		}
	}
	if hasDanger {
		names := dangerHits
		if len(names) == 0 {
			names = []string{root.Name}
		}
		job.addChecks([]Check{{ID: "DEV-DANGER", Stage: "A", Label: "系统组件", Level: "warn",
			Detail: "设备侧解析出的计划包含系统组件：" + strings.Join(names, "、") + "；安装后可能导致设备无法启动"}})
	}
	if !req.Commit {
		fail("WRITE_NOT_AUTHORIZED", "真实安装必须设置 commit=true")
		return
	}
	if err := a.authorizeDeviceWrite(req, hasDanger); err != nil {
		fail("WRITE_NOT_AUTHORIZED", err.Error())
		return
	}

	// 真实安装：写前快照 -> 设备侧安装 -> 装后回读。
	job.setPhase("install", 80)
	emit()
	before, err := a.readDeviceState(ctx)
	if err != nil {
		fail("READBACK_FAILED", "安装前快照失败："+err.Error())
		return
	}
	job.addLine(fmt.Sprintf("安装前快照完成：已装 %d 个包", len(before.Packages)))

	installCmd := "apk add " + shq(target)
	if env.Manager == "opkg" {
		installCmd = "opkg install " + shq(target)
	}
	job.addLine("$ " + installCmd)
	iout, ierr := a.router.execJob(ctx, installCmd, 600*time.Second, job.addLine)
	if ierr != nil || iout.ExitCode != 0 {
		msg := fmt.Sprintf("设备侧安装失败（命令：%s，退出码 %d）：%s",
			installCmd, iout.ExitCode, tailText(iout.Output, 300))
		after, _ := a.readDeviceState(context.Background())
		job.finish(&InstallResult{Code: "INSTALL_FAILED", Message: msg,
			Simulate: simCmd + "\n" + simOut, Rollbackable: true, Failed: root.Name,
			Changes: diffDeviceState(before, after)})
		emit()
		return
	}

	job.setPhase("readback", 97)
	emit()
	after, rerr := a.readDeviceState(ctx)
	if rerr != nil {
		job.finish(&InstallResult{Code: "READBACK_FAILED", Message: "安装已完成，但装后回读失败：" + rerr.Error(),
			Simulate: simCmd + "\n" + simOut, Rollbackable: true})
		emit()
		return
	}
	changes := diffDeviceState(before, after)
	installed := changes.PackagesAdded
	if len(installed) == 0 && root.Name != "" {
		installed = []string{root.Name + " " + root.Version}
	}
	msg := fmt.Sprintf("设备侧解析并安装完成：包列表新增 %d，init.d 新增 %d，配置变化 %d。未重启任何服务。",
		len(changes.PackagesAdded), len(changes.InitAdded), len(changes.ConfigChanged))
	job.addLine(msg)
	job.finish(&InstallResult{Success: true, Code: "INSTALL_OK", Message: msg,
		Simulate: simCmd + "\n" + simOut, Installed: installed, Changes: changes, Rollbackable: true})
	emit()
}

// deviceSimulate 让设备自己解析依赖并预演，不下载安装。
func (a *App) deviceSimulate(ctx context.Context, env *PkgEnv, target string, logf func(string)) (string, string, error) {
	var cmd string
	if env.Manager == "opkg" {
		cmd = "opkg install --noaction " + shq(target)
	} else {
		cmd = "apk add --simulate " + shq(target)
	}
	if logf != nil {
		logf("$ " + cmd)
	}
	out, err := a.router.execJob(ctx, cmd, 240*time.Second, logf)
	if err != nil {
		return cmd, out.Output, err
	}
	if out.ExitCode == 0 {
		return cmd, out.Output, nil
	}
	// 设备上没有 apk 索引缓存时，apk add 不会自动下载索引，会报 opening from cache/no such package。
	// 这里先做一次 apk update，再重试同一条 simulate（依赖仍由设备解析）。
	low := strings.ToLower(out.Output)
	if env.Manager == "apk" && (strings.Contains(low, "opening from cache") ||
		strings.Contains(low, "no such package") || strings.Contains(low, "unable to select packages")) {
		upd := "apk update"
		if logf != nil {
			logf("设备缺少 apk 索引缓存，先执行 " + upd)
		}
		if uout, uerr := a.router.execJob(ctx, upd, 240*time.Second, logf); uerr != nil || uout.ExitCode != 0 {
			return cmd, out.Output + "\n" + uout.Output, fmt.Errorf("apk update 失败：%s", tailText(uout.Output, 200))
		}
		if logf != nil {
			logf("$ " + cmd)
		}
		if out2, err2 := a.router.execJob(ctx, cmd, 240*time.Second, logf); err2 == nil && out2.ExitCode == 0 {
			return cmd, out2.Output, nil
		} else if err2 != nil {
			return cmd, out2.Output, err2
		} else {
			return cmd, out2.Output, fmt.Errorf("simulate 退出码 %d：%s", out2.ExitCode, tailText(out2.Output, 300))
		}
	}
	return cmd, out.Output, fmt.Errorf("simulate 退出码 %d：%s", out.ExitCode, tailText(out.Output, 300))
}

var pkgTokenRe = regexp.MustCompile(`[a-z0-9][a-z0-9+._:-]*`)

// dangerousNamesInText 从 simulate 输出里挑出系统组件名，用于二次确认提示。
func dangerousNamesInText(out string) []string {
	seen := map[string]bool{}
	res := []string{}
	for _, tok := range pkgTokenRe.FindAllString(strings.ToLower(out), -1) {
		tok = strings.Trim(tok, "+._:-")
		if tok == "" || seen[tok] || !dangerousPackage(tok) {
			continue
		}
		seen[tok] = true
		res = append(res, tok)
	}
	return res
}

// kernelRelatedItem 判断计划项是否属于内核相关包：kmod、内核本体，或直接依赖 kernel。
func kernelRelatedItem(it *PlanItem) bool {
	n := strings.ToLower(strings.TrimSpace(it.Name))
	if strings.HasPrefix(n, "kmod-") || n == "kmod" || n == "kernel" || strings.HasPrefix(n, "kernel-") ||
		strings.HasPrefix(n, "linux-") {
		return true
	}
	for _, d := range it.Depends {
		name, _, _ := depNameOp(strings.TrimPrefix(strings.TrimSpace(d), "!"))
		name = strings.ToLower(name)
		if name == "kernel" || name == "kmod" || strings.HasPrefix(name, "kmod-") {
			return true
		}
	}
	return false
}

// pcModeBlocked 返回本机闭包模式必须拒绝的包名。空列表表示允许。
func pcModeBlocked(plan *InstallPlan) []string {
	if plan == nil {
		return nil
	}
	out := []string{}
	for _, it := range plan.Packages {
		if it.Installed {
			continue
		}
		if kernelRelatedItem(it) {
			out = append(out, it.Name)
		}
	}
	return out
}

func pcModeBlockedMessage(names []string) string {
	return "本机闭包模式拒绝安装内核相关包：" + strings.Join(names, "、") +
		"。本机下载的内核模块可能与设备内核哈希不匹配，装了不加载甚至起不来；" +
		"请切换“设备侧解析”，由设备的 apk 按内核哈希选择 kmod。"
}

// classifyFailure 把失败原因归到五类可读中文：源不可达、依赖缺失、签名不受信、
// 架构或内核哈希不匹配、空间不足。
func classifyFailure(code, msg string) (string, string) {
	low := strings.ToLower(code + " " + msg)
	switch {
	case strings.Contains(low, "space") || strings.Contains(low, "enospc") || strings.Contains(low, "no space"):
		return "空间不足", "设备 overlay 或 /tmp 可写空间不足，请先清理空间再安装。"
	case strings.Contains(low, "untrusted") || strings.Contains(low, "signature") || strings.Contains(low, "key"):
		return "签名不受信", "包的签名不被设备信任，设备拒绝了安装；请为设备添加对应公钥，或确认包来源后再处理。"
	case strings.Contains(low, "arch") || strings.Contains(low, "kernel") || strings.Contains(low, "abi"):
		return "架构或内核哈希不匹配", "包的架构或内核 ABI 与设备不一致；请换用设备对应架构、对应内核版本的包。"
	case strings.Contains(low, "unreachable") || strings.Contains(low, "network") || strings.Contains(low, "timeout") ||
		strings.Contains(low, "connection") || strings.Contains(low, "connect:") || strings.Contains(low, "refused") ||
		strings.Contains(low, "http 4") || strings.Contains(low, "http 5") || strings.Contains(low, "404") ||
		strings.Contains(low, "503") || strings.Contains(low, "dial tcp"):
		return "源不可达", "设备或包源无法访问：检查设备网络、仓库地址与 DNS 后重试。"
	case strings.Contains(low, "depend") || strings.Contains(low, "missing") || strings.Contains(low, "not found") ||
		strings.Contains(low, "unresolved") || strings.Contains(low, "conflict") || strings.Contains(low, "satisfies"):
		return "依赖缺失", "索引或设备源里找不到包或依赖版本；请刷新索引，或改用设备侧解析让设备按当前源补齐依赖。"
	}
	return "安装失败", "安装没有完成，请展开日志查看设备的原始报错。"
}
