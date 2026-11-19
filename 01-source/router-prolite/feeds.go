package main

import (
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"
)

// Feed 是设备上配置的包源（只读解析，不修改设备）。
type Feed struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// InstalledPkg 是设备已安装包的最小信息。
type InstalledPkg struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Arch    string `json:"arch"`
}

// PkgEnv 是安装链路需要的设备包环境快照。
type PkgEnv struct {
	Manager    string                  `json:"manager"`
	Arch       string                  `json:"arch"`
	Kernel     string                  `json:"kernel"`
	KernelPkg  string                  `json:"kernelPkg"`
	Release    string                  `json:"release"`
	Feeds      []Feed                  `json:"feeds"`
	Installed  map[string]InstalledPkg `json:"installed"`
	ApkKeys    map[string]string       `json:"-"`
	OverlayKB  int64                   `json:"overlayKb"`
	TmpKB      int64                   `json:"tmpKb"`
	SftpServer string                  `json:"sftpServer"`
	FetchedAt  int64                   `json:"fetchedAt"`
	Notes      []string                `json:"notes"`
}

const pkgEnvScript = `echo '@@PM'; command -v apk 2>/dev/null; command -v opkg 2>/dev/null
echo '@@ARCH'; cat /etc/apk/arch 2>/dev/null; apk --print-arch 2>/dev/null; opkg print-architecture 2>/dev/null
echo '@@KERNEL'; uname -r 2>/dev/null
echo '@@KPKG'; apk info kernel 2>/dev/null; opkg info kernel 2>/dev/null
echo '@@APKREPO'; cat /etc/apk/repositories 2>/dev/null; for f in /etc/apk/repositories.d/*; do [ -f "$f" ] && cat "$f"; done 2>/dev/null
echo '@@OPKGREPO'; cat /etc/opkg/distfeeds.conf 2>/dev/null; cat /etc/opkg/customfeeds.conf 2>/dev/null
echo '@@DBCOUNT'; echo "apk $(grep -c '^P:' /lib/apk/db/installed 2>/dev/null)"; echo "opkg $(grep -c '^Package:' /usr/lib/opkg/status 2>/dev/null)"
echo '@@DF'; df -k /overlay /tmp / 2>/dev/null
echo '@@SFTP'; for p in /usr/libexec/sftp-server /usr/lib/sftp-server /usr/lib/ssh/sftp-server; do [ -x "$p" ] && echo "$p"; done
echo '@@APKKEY'; for f in /etc/apk/keys/*; do [ -f "$f" ] && { echo "@@KEYFILE $f"; cat "$f" 2>/dev/null; echo "@@KEYEND"; }; done
echo '@@REL'; cat /etc/openwrt_release 2>/dev/null
echo '@@END'; true`

// ReadPkgEnv 只读采集包管理器、架构、内核、源与已装列表。
func (r *Router) ReadPkgEnv() (*PkgEnv, error) {
	client, err := r.ensureClient()
	if err != nil {
		return nil, err
	}
	out, _, err := execWithClient(client, pkgEnvScript, 20*time.Second)
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, fmt.Errorf("读取设备包环境失败：%w", err)
	}
	sec := splitSections(out)
	env := &PkgEnv{
		Installed: map[string]InstalledPkg{},
		ApkKeys:   map[string]string{},
		FetchedAt: time.Now().UnixMilli(),
	}

	apkPath := firstLine(sec["PM"], 0)
	opkgPath := firstLine(sec["PM"], 1)
	apkCount, opkgCount := envDBCounts(sec["DBCOUNT"])
	switch {
	case apkPath != "" && opkgPath == "":
		env.Manager = "apk"
	case opkgPath != "" && apkPath == "":
		env.Manager = "opkg"
	case apkPath != "" && opkgPath != "":
		if apkCount >= opkgCount {
			env.Manager = "apk"
			env.Notes = append(env.Notes, fmt.Sprintf("同时存在 apk 与 opkg，按包库条目数选择 apk（%d >= %d）", apkCount, opkgCount))
		} else {
			env.Manager = "opkg"
			env.Notes = append(env.Notes, fmt.Sprintf("同时存在 apk 与 opkg，按包库条目数选择 opkg（%d > %d）", opkgCount, apkCount))
		}
	default:
		env.Manager = ""
	}

	env.Arch = envArch(sec["ARCH"])
	env.Kernel = strings.TrimSpace(sec["KERNEL"])
	env.KernelPkg = envKernelPkg(sec["KPKG"])
	if env.KernelPkg == "" && env.Kernel != "" {
		env.KernelPkg = env.Kernel
	}
	env.OverlayKB, env.TmpKB = parseDiskSpace(sec["DF"])
	env.SftpServer = firstLine(sec["SFTP"], 0)
	env.Release = envRelease(sec["REL"])
	env.ApkKeys = envKeyFiles(out)

	if env.Manager == "apk" {
		env.Feeds = parseApkRepos(sec["APKREPO"])
	} else if env.Manager == "opkg" {
		env.Feeds = parseOpkgRepos(sec["OPKGREPO"])
	} else {
		env.Feeds = append(parseApkRepos(sec["APKREPO"]), parseOpkgRepos(sec["OPKGREPO"])...)
	}

	if env.Manager != "" {
		if err := r.readInstalled(env); err != nil {
			env.Notes = append(env.Notes, "读取已装包列表失败："+err.Error())
		}
	}
	if env.Manager == "" {
		env.Notes = append(env.Notes, "未检测到 apk 或 opkg，安装功能不可用")
	}
	if env.Arch == "" {
		env.Notes = append(env.Notes, "未能读取设备架构")
	}
	return env, nil
}

func (r *Router) readInstalled(env *PkgEnv) error {
	client, err := r.ensureClient()
	if err != nil {
		return err
	}
	var cmd string
	if env.Manager == "apk" {
		cmd = "apk info -v 2>/dev/null"
	} else {
		cmd = "opkg list-installed 2>/dev/null"
	}
	out, _, err := execWithClient(client, cmd, 20*time.Second)
	if err != nil && strings.TrimSpace(out) == "" {
		return err
	}
	if env.Manager == "apk" && strings.TrimSpace(out) == "" {
		alt, _, aerr := execWithClient(client, "apk list --installed 2>/dev/null", 20*time.Second)
		if aerr == nil || strings.TrimSpace(alt) != "" {
			out = alt
		}
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, version := "", ""
		if idx := strings.Index(line, " - "); idx > 0 {
			name = strings.TrimSpace(line[:idx])
			version = strings.TrimSpace(line[idx+3:])
		} else if m := apkPkgRe.FindStringSubmatch(line); m != nil {
			name, version = m[1], m[2]
		} else if f := strings.Fields(line); len(f) >= 2 {
			name, version = f[0], f[1]
		}
		if name != "" {
			env.Installed[name] = InstalledPkg{Name: name, Version: version}
		}
	}
	return nil
}

func firstLine(block string, idx int) string {
	lines := strings.Split(strings.TrimSpace(block), "\n")
	var out []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}
	if idx < len(out) {
		return out[idx]
	}
	return ""
}

func envDBCounts(block string) (int, int) {
	var apk, opkg int
	for _, line := range strings.Split(block, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		n, _ := strconv.Atoi(f[1])
		switch f[0] {
		case "apk":
			apk = n
		case "opkg":
			opkg = n
		}
	}
	return apk, opkg
}

func envArch(block string) string {
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "arch ") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				return f[1]
			}
		}
		if !strings.Contains(line, " ") && !strings.Contains(line, ":") {
			return line
		}
	}
	return ""
}

func envKernelPkg(block string) string {
	block = strings.TrimSpace(block)
	if block == "" {
		return ""
	}
	for _, line := range strings.Split(block, "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(l), "version:") {
			return strings.TrimSpace(l[len("version:"):])
		}
	}
	// apk info kernel 输出形如 kernel-6.18.26~hash-r1
	first := firstLine(block, 0)
	if strings.HasPrefix(first, "kernel-") {
		return strings.TrimPrefix(first, "kernel-")
	}
	return first
}

func parseDiskSpace(block string) (overlayKB, tmpKB int64) {
	var rootKB int64
	for _, line := range strings.Split(block, "\n") {
		f := strings.Fields(line)
		if len(f) < 6 || f[0] == "Filesystem" {
			continue
		}
		avail, err := strconv.ParseInt(f[3], 10, 64)
		if err != nil {
			continue
		}
		mnt := f[len(f)-1]
		switch mnt {
		case "/overlay":
			overlayKB = avail
		case "/tmp":
			tmpKB = avail
		case "/":
			rootKB = avail
		}
	}
	if overlayKB == 0 {
		overlayKB = rootKB
	}
	return overlayKB, tmpKB
}

func envRelease(block string) string {
	kv := parseShellKV(block)
	if kv["DISTRIB_DESCRIPTION"] != "" {
		return kv["DISTRIB_DESCRIPTION"]
	}
	if kv["DISTRIB_ID"] != "" {
		return kv["DISTRIB_ID"] + " " + kv["DISTRIB_RELEASE"]
	}
	return strings.TrimSpace(firstLine(block, 0))
}

func parseApkRepos(block string) []Feed {
	out := []Feed{}
	seen := map[string]bool{}
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		tag := ""
		if strings.HasPrefix(line, "@") {
			f := strings.Fields(line)
			if len(f) < 2 {
				continue
			}
			tag = strings.TrimPrefix(f[0], "@")
			line = f[1]
		}
		if !strings.HasPrefix(line, "http://") && !strings.HasPrefix(line, "https://") {
			continue
		}
		line = strings.TrimRight(line, "/")
		if seen[line] {
			continue
		}
		seen[line] = true
		name := tag
		if name == "" {
			name = path.Base(line)
		}
		out = append(out, Feed{Name: name, URL: line})
	}
	return out
}

func parseOpkgRepos(block string) []Feed {
	out := []Feed{}
	seen := map[string]bool{}
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 3 || (f[0] != "src/gz" && f[0] != "src") {
			continue
		}
		url := strings.TrimRight(f[2], "/")
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			continue
		}
		if seen[url] {
			continue
		}
		seen[url] = true
		out = append(out, Feed{Name: f[1], URL: url})
	}
	return out
}

func envKeyFiles(raw string) map[string]string {
	out := map[string]string{}
	lines := strings.Split(raw, "\n")
	name := ""
	var b strings.Builder
	for _, line := range lines {
		if strings.HasPrefix(line, "@@KEYFILE ") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "@@KEYFILE "))
			b.Reset()
			continue
		}
		if line == "@@KEYEND" {
			if name != "" {
				out[path.Base(name)] = b.String()
			}
			name = ""
			continue
		}
		if name != "" {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return out
}
