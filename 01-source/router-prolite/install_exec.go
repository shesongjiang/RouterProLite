package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// installLogPath 返回本地安装日志路径，便于事后排查。
func installLogPath(jobID string) string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = "."
	}
	dir := filepath.Join(base, "ProLite", "logs")
	_ = os.MkdirAll(dir, 0o700)
	return filepath.Join(dir, "install-"+jobID+".log")
}

func appendInstallLog(path, line string) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(time.Now().Format("2006-01-02 15:04:05") + "  " + line + "\n")
}

type deviceState struct {
	Packages map[string]string
	Init     map[string]bool
	Config   map[string]string
}

const deviceStateScript = `echo '@@PKGS'
if command -v apk >/dev/null 2>&1; then apk info -v 2>/dev/null || apk list --installed 2>/dev/null; else opkg list-installed 2>/dev/null; fi
echo '@@INIT'
ls /etc/init.d 2>/dev/null
echo '@@CFG'
for f in /etc/config/*; do [ -f "$f" ] && sha256sum "$f" 2>/dev/null; done
echo '@@END'
true`

func (a *App) readDeviceState(ctx context.Context) (*deviceState, error) {
	out, err := a.router.execJob(ctx, deviceStateScript, 60*time.Second, nil)
	if err != nil {
		return nil, err
	}
	if out.ExitCode != 0 {
		return nil, fmt.Errorf("回读命令退出码 %d：%s", out.ExitCode, tailText(out.Output, 200))
	}
	sec := splitSections(out.Output)
	st := &deviceState{
		Packages: parseInstalledState(sec["PKGS"]),
		Init:     map[string]bool{},
		Config:   map[string]string{},
	}
	for _, line := range strings.Split(sec["INIT"], "\n") {
		name := strings.TrimSpace(line)
		if name != "" {
			st.Init[name] = true
		}
	}
	for _, line := range strings.Split(sec["CFG"], "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 2 {
			st.Config[fields[1]] = fields[0]
		}
	}
	return st, nil
}

func parseInstalledState(text string) map[string]string {
	res := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if idx := strings.Index(line, " - "); idx > 0 {
			name := strings.TrimSpace(line[:idx])
			ver := strings.TrimSpace(line[idx+3:])
			if i := strings.Index(ver, " - "); i > 0 {
				ver = strings.TrimSpace(ver[:i])
			}
			res[name] = ver
			continue
		}
		if m := apkPkgRe.FindStringSubmatch(line); m != nil {
			res[m[1]] = m[2]
			continue
		}
		if f := strings.Fields(line); len(f) >= 2 {
			res[f[0]] = f[1]
		}
	}
	return res
}

func diffDeviceState(before, after *deviceState) *InstallChanges {
	ch := &InstallChanges{PackagesAdded: []string{}, InitAdded: []string{}, ConfigChanged: []string{}}
	if before == nil || after == nil {
		return ch
	}
	for name, ver := range after.Packages {
		if _, ok := before.Packages[name]; !ok {
			ch.PackagesAdded = append(ch.PackagesAdded, name+" "+ver)
		}
	}
	for name := range after.Init {
		if !before.Init[name] {
			ch.InitAdded = append(ch.InitAdded, name)
		}
	}
	for path, hash := range after.Config {
		if before.Config[path] != hash {
			ch.ConfigChanged = append(ch.ConfigChanged, path)
		}
	}
	sort.Strings(ch.PackagesAdded)
	sort.Strings(ch.InitAdded)
	sort.Strings(ch.ConfigChanged)
	return ch
}

func pendingPackageNames(plan *InstallPlan, from int) []string {
	out := []string{}
	for _, it := range plan.Packages[minInt(from, len(plan.Packages)):] {
		if !it.Installed {
			out = append(out, it.Name)
		}
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// installOne 只对单个本地包执行 apk/opkg 安装。
// apk 的 --allow-untrusted 仅在设备返回 UNTRUSTED 且桌面端已做 SHA256 校验时使用。
func (a *App) installOne(ctx context.Context, env *PkgEnv, it *PlanItem, logf func(string)) (string, JobOutput, error) {
	file := shq(it.RemotePath)
	var cmd string
	if env.Manager == "opkg" {
		cmd = "opkg install " + file
	} else {
		// apk-tools 3.x 没有 --no-network：用空仓库文件确保不联网。
		cmd = "apk --repositories-file /dev/null add " + file
	}
	logf("$ " + cmd)
	out, err := a.router.execJob(ctx, cmd, 300*time.Second, logf)
	if err != nil {
		return cmd, out, err
	}
	if out.ExitCode == 0 || env.Manager == "opkg" {
		return cmd, out, nil
	}
	low := strings.ToLower(out.Output)
	if strings.Contains(low, "untrusted") || strings.Contains(low, "no signature") {
		alt := "apk --repositories-file /dev/null add --allow-untrusted " + file
		logf("检测到 UNTRUSTED 签名；该包 SHA256 已在桌面端校验，仅对这一包使用 --allow-untrusted 重试")
		logf("$ " + alt)
		out2, err2 := a.router.execJob(ctx, alt, 300*time.Second, logf)
		return alt, out2, err2
	}
	if strings.Contains(low, "unrecognized option") || strings.Contains(low, "unknown option") ||
		strings.Contains(low, "usage") {
		// 仅旧版 apk 回退分支：apk-tools 3.0.5 之前可能存在 --no-network。
		alt := "apk add --no-network " + file
		logf("当前 apk 不支持 --repositories-file，回退旧版 --no-network 分支")
		logf("$ " + alt)
		out2, err2 := a.router.execJob(ctx, alt, 300*time.Second, logf)
		return alt, out2, err2
	}
	return cmd, out, nil
}

// executeInstall 真实写入：安装前快照 -> 逐包安装（依赖在前）-> 装后回读与差异。
func (a *App) executeInstall(ctx context.Context, job *InstallJob, env *PkgEnv, plan *InstallPlan,
	simCmd string, emit func()) *InstallResult {

	logf := job.addLine
	logf("真实安装开始：目标设备 " + a.router.TargetHost() + "，共 " + fmt.Sprint(len(plan.Packages)) + " 个计划项")
	// 写入前最后一道硬闸：危险包直接拒绝，任何确认参数都不能绕过。
	for _, it := range plan.Packages {
		if !it.Installed && dangerousPackage(it.Name) {
			msg := "危险包被硬性拒绝：" + it.Name + "（kmod/内核/引导/base-files/libc/apk/opkg）"
			logf(msg)
			return &InstallResult{Code: "DANGER_FORBIDDEN", Message: msg, Simulate: simCmd}
		}
	}
	before, err := a.readDeviceState(ctx)
	if err != nil {
		return &InstallResult{Code: "READBACK_FAILED", Message: "安装前快照失败：" + err.Error(), Simulate: simCmd}
	}
	logf("安装前快照完成：已装 " + fmt.Sprint(len(before.Packages)) + " 个包，init.d " + fmt.Sprint(len(before.Init)) + " 项")

	installed := []string{}
	for idx, it := range plan.Packages {
		if ctx.Err() != nil {
			return a.cancelledInstallResult(job, before, installed, plan, idx, simCmd)
		}
		if it.Installed {
			logf("跳过 " + it.Name + "：设备已安装相同或更新版本")
			job.setFile(it.Name, "installed", it.Size, it.Size)
			continue
		}
		if it.RemotePath == "" {
			res := &InstallResult{Code: "TRANSFER_IO", Message: it.Name + " 缺少远端路径",
				Installed: installed, Failed: it.Name, Pending: pendingPackageNames(plan, idx)}
			job.setFile(it.Name, "failed", 0, it.Size)
			return res
		}
		job.setPhase("install", 90)
		job.setFile(it.Name, "installing", 0, it.Size)
		emit()
		logf(fmt.Sprintf("安装 %s %s（%d/%d）", it.Name, it.Version, idx+1, len(plan.Packages)))
		cmd, out, ierr := a.installOne(ctx, env, it, logf)
		if ctx.Err() != nil {
			return a.cancelledInstallResult(job, before, installed, plan, idx, simCmd)
		}
		if ierr != nil || out.ExitCode != 0 {
			msg := fmt.Sprintf("包 %s 安装失败（命令：%s，退出码 %d）：%s",
				it.Name, cmd, out.ExitCode, tailText(out.Output, 260))
			job.setFile(it.Name, "failed", 0, it.Size)
			after, _ := a.readDeviceState(context.Background())
			logf("失败：" + msg)
			return &InstallResult{
				Code: "INSTALL_FAILED", Message: msg, Simulate: simCmd, Rollbackable: true,
				Installed: installed, Failed: it.Name, Pending: pendingPackageNames(plan, idx),
				Changes: diffDeviceState(before, after),
			}
		}
		installed = append(installed, it.Name+" "+it.Version)
		job.setFile(it.Name, "installed", it.Size, it.Size)
		logf("完成 " + it.Name + "（退出码 0）")
		emit()
	}

	job.setPhase("readback", 97)
	emit()
	logf("所有包安装完成，开始装后回读")
	after, rerr := a.readDeviceState(ctx)
	if rerr != nil {
		return &InstallResult{Code: "READBACK_FAILED", Message: "安装已完成，但装后回读失败：" + rerr.Error(),
			Simulate: simCmd, Installed: installed, Rollbackable: true}
	}
	changes := diffDeviceState(before, after)
	msg := fmt.Sprintf("已安装 %d 个包；包列表新增 %d，init.d 新增 %d，配置变化 %d。未重启任何服务。",
		len(installed), len(changes.PackagesAdded), len(changes.InitAdded), len(changes.ConfigChanged))
	logf(msg)
	return &InstallResult{Success: true, Code: "INSTALL_OK", Message: msg, Simulate: simCmd,
		Installed: installed, Pending: []string{}, Changes: changes, Rollbackable: true}
}

func (a *App) cancelledInstallResult(job *InstallJob, before *deviceState, installed []string,
	plan *InstallPlan, idx int, simCmd string) *InstallResult {

	job.addLine("用户取消：停止后续安装，已完成的包保留，不自动卸载")
	after, _ := a.readDeviceState(context.Background())
	ch := diffDeviceState(before, after)
	return &InstallResult{
		Code: "CANCELLED", Message: fmt.Sprintf("已取消；成功 %d 个，后续 %d 个未执行",
			len(installed), len(pendingPackageNames(plan, idx))),
		Simulate: simCmd, Installed: installed, Pending: pendingPackageNames(plan, idx),
		Changes: ch, Rollbackable: true,
	}
}

// CleanupInstallJob 仅清理远端临时文件；不卸载任何已安装包。
func (a *App) CleanupInstallJob(id string) error {
	id = strings.TrimSpace(id)
	a.jobsMu.Lock()
	job := a.jobs[id]
	a.jobsMu.Unlock()
	if job == nil {
		return fmt.Errorf("任务不存在")
	}
	if job.view().Running {
		return fmt.Errorf("任务仍在运行，请先取消后再清理")
	}
	a.router.cleanupRemoteDir(context.Background(), id)
	job.addLine("已按请求清理远端临时文件（未卸载任何包）")
	return nil
}
