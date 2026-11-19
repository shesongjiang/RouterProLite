package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// PkgMeta 是桌面端对 .ipk / .apk 的本地解析结果。
type PkgMeta struct {
	Format        string   `json:"format"`
	Path          string   `json:"path"`
	Name          string   `json:"name"`
	Version       string   `json:"version"`
	Arch          string   `json:"arch"`
	Size          int64    `json:"size"`
	InstalledSize int64    `json:"installedSize"`
	Depends       []string `json:"depends"`
	Provides      []string `json:"provides"`
	Conflicts     []string `json:"conflicts"`
	Replaces      []string `json:"replaces"`
	HasScripts    bool     `json:"hasScripts"`
	SHA256        string   `json:"sha256"`
	SizeEstimated bool     `json:"sizeEstimated"`
}

// DetectPackage 通过魔数判断包格式。
func DetectPackage(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("无法打开文件：%w", err)
	}
	defer f.Close()
	head := make([]byte, 8)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	if bytes.HasPrefix(head, []byte("!<arch>\n")) {
		return "ipk", nil
	}
	if len(head) >= 2 && head[0] == 0x1f && head[1] == 0x8b {
		return "apk", nil
	}
	if bytes.HasPrefix(head, []byte("ADB")) {
		return "apk3", nil
	}
	return "", errors.New("不是可安装的 .ipk / .apk 文件")
}

// ParseIPK 解析 ar 归档格式的 .ipk。
func ParseIPK(path string) (*PkgMeta, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("无法打开文件：%w", err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	meta := &PkgMeta{Format: "ipk", Path: path, Size: st.Size()}
	meta.SHA256, _ = fileSHA256(path)

	hdr := make([]byte, 8)
	if _, err := io.ReadFull(f, hdr); err != nil || string(hdr) != "!<arch>\n" {
		return nil, errors.New("不是有效的 ipk（ar 头不正确）")
	}

	controlFound := false
	for {
		h := make([]byte, 60)
		if _, err := io.ReadFull(f, h); err != nil {
			break
		}
		name := strings.TrimSpace(string(h[0:16]))
		sizeStr := strings.TrimSpace(string(h[48:58]))
		size, err := strconv.ParseInt(sizeStr, 10, 64)
		if err != nil || size < 0 || size > 64*1024*1024 {
			return nil, errors.New("ipk 归档头损坏")
		}
		data := make([]byte, size)
		if _, err := io.ReadFull(f, data); err != nil {
			return nil, errors.New("ipk 内容不完整")
		}
		if size%2 == 1 {
			_, _ = f.Seek(1, io.SeekCurrent)
		}
		if strings.HasPrefix(name, "control.tar") {
			if !strings.HasSuffix(name, ".gz") {
				return nil, fmt.Errorf("不支持的 control 压缩格式：%s", name)
			}
			fields, err := parseIPKControl(data)
			if err != nil {
				return nil, err
			}
			fillPkgMeta(meta, fields)
			controlFound = true
		}
	}
	if !controlFound {
		return nil, errors.New("ipk 中未找到 control.tar.gz")
	}
	if meta.Name == "" || meta.Version == "" {
		return nil, errors.New("ipk control 缺少 Package / Version 字段")
	}
	return meta, nil
}

func parseIPKControl(data []byte) (map[string]string, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("control.tar.gz 解压失败：%w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("control.tar.gz 读取失败：%w", err)
		}
		base := strings.TrimPrefix(strings.TrimPrefix(h.Name, "./"), "/")
		if base != "control" {
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(tr, 1<<20))
		if err != nil {
			return nil, err
		}
		return parseControlText(string(raw)), nil
	}
	return nil, errors.New("control.tar.gz 中未找到 control 文件")
}

func parseControlText(raw string) map[string]string {
	out := map[string]string{}
	var key string
	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if key != "" {
				out[key] += " " + strings.TrimSpace(line)
			}
			continue
		}
		idx := strings.Index(line, ":")
		if idx <= 0 {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(line[:idx]))
		out[key] = strings.TrimSpace(line[idx+1:])
	}
	return out
}

func fillPkgMeta(meta *PkgMeta, f map[string]string) {
	meta.Name = f["package"]
	meta.Version = f["version"]
	meta.Arch = f["architecture"]
	meta.InstalledSize = parseSizeField(f["installed-size"])
	meta.Depends = splitCommaList(f["depends"])
	meta.Provides = splitCommaList(f["provides"])
	meta.Conflicts = splitCommaList(f["conflicts"])
	meta.Replaces = splitCommaList(f["replaces"])
	if f["installed-size"] == "" {
		meta.SizeEstimated = true
	}
	meta.HasScripts = f["preinst"] != "" || f["postinst"] != "" || f["prerm"] != "" || f["postrm"] != ""
}

// ParseAPK 解析 apk（gzip tar，含 .PKGINFO）。
func ParseAPK(path string) (*PkgMeta, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("无法打开文件：%w", err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	meta := &PkgMeta{Format: "apk", Path: path, Size: st.Size()}
	meta.SHA256, _ = fileSHA256(path)

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, errors.New("不是有效的 apk（gzip 解压失败）")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	raw := ""
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("apk 读取失败：%w", err)
		}
		base := strings.TrimPrefix(strings.TrimPrefix(h.Name, "./"), "/")
		if base == ".PKGINFO" {
			b, err := io.ReadAll(io.LimitReader(tr, 1<<20))
			if err != nil {
				return nil, err
			}
			raw = string(b)
		}
		if strings.HasSuffix(base, ".pre-install") || strings.HasSuffix(base, ".post-install") ||
			strings.HasSuffix(base, ".pre-deinstall") || strings.HasSuffix(base, ".post-deinstall") {
			meta.HasScripts = true
		}
	}
	if raw == "" {
		return nil, errors.New("apk 中未找到 .PKGINFO")
	}
	info := parsePkgInfo(raw)
	meta.Name = info["pkgname"]
	meta.Version = info["pkgver"]
	meta.Arch = info["arch"]
	meta.InstalledSize = parseSizeField(info["size"])
	meta.Depends = splitList(info["depend"])
	meta.Provides = splitList(info["provides"])
	meta.Replaces = splitList(info["replaces"])
	if meta.InstalledSize == 0 {
		meta.SizeEstimated = true
	}
	if meta.Name == "" || meta.Version == "" {
		return nil, errors.New("apk .PKGINFO 缺少 pkgname / pkgver")
	}
	return meta, nil
}

func parsePkgInfo(raw string) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx <= 0 {
			continue
		}
		k := strings.TrimSpace(line[:idx])
		v := strings.TrimSpace(line[idx+1:])
		if prev, ok := out[k]; ok && prev != "" {
			out[k] = prev + " " + v
		} else {
			out[k] = v
		}
	}
	return out
}

func splitList(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	out := []string{}
	for _, part := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\t'
	}) {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// splitCommaList 只按逗号分隔，保留 `foo (>= 1.2)` 这类带空格的约束。
func splitCommaList(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	out := []string{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(strings.ReplaceAll(part, "\n", " "))
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func parseSizeField(s string) int64 {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "B")
	n, _ := strconv.ParseInt(s, 10, 64)
	if n < 0 {
		return 0
	}
	return n
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ParseAPK3 解析 apk-tools 3.x 的 ADB 容器包（schema=pckg）。
// 包内第一个 data 块是元数据对象，字段布局与 packages.adb 索引里的包对象一致。
func ParseAPK3(path string) (*PkgMeta, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("无法打开文件：%w", err)
	}
	meta := &PkgMeta{Format: "apk", Path: path, Size: int64(len(raw))}
	meta.SHA256, _ = fileSHA256(path)

	body, err := adbDecompress(raw)
	if err != nil {
		return nil, fmt.Errorf("不是有效的 apk v3 包：%w", err)
	}
	if len(body) < 8 || string(body[4:8]) != "pckg" {
		return nil, fmt.Errorf("apk v3 schema 不是 pckg：%q", string(body[4:minInt(8, len(body))]))
	}
	blocks, err := adbReadBlocks(body)
	if err != nil {
		return nil, err
	}
	var metaBlock []byte
	for _, b := range blocks {
		if b.typ == 0 {
			metaBlock = b.payload
			break
		}
	}
	if len(metaBlock) < 8 {
		return nil, errors.New("apk v3 包缺少元数据块")
	}
	p := &adbParser{data: metaBlock}
	rootFields := p.obj(p.u32(4))
	if len(rootFields) == 0 {
		return nil, errors.New("apk v3 元数据根对象为空")
	}
	fields := p.obj(rootFields[0])
	if len(fields) < 5 {
		return nil, errors.New("apk v3 元数据缺少包字段")
	}
	meta.Name = p.str(fields[0])
	meta.Version = p.str(fields[1])
	meta.Arch = p.str(fields[4])
	if len(fields) > 11 {
		meta.InstalledSize = p.intVal(fields[11])
	}
	if len(fields) > 14 {
		meta.Depends = p.deps(fields[14])
	}
	if len(fields) > 15 {
		meta.Provides = p.deps(fields[15])
	}
	if len(fields) > 16 {
		meta.Replaces = p.deps(fields[16])
	}
	if meta.InstalledSize == 0 {
		meta.SizeEstimated = true
	}
	if len(rootFields) > 2 {
		for i, sf := range p.obj(rootFields[2]) {
			if (i == 2 || i == 3 || i == 6) && len(p.blob(sf)) > 0 {
				meta.HasScripts = true
				break
			}
		}
	}
	if meta.Name == "" || meta.Version == "" {
		return nil, errors.New("apk v3 元数据缺少 pkgname / pkgver")
	}
	return meta, nil
}

// ParsePackage 自动识别格式并解析。
func ParsePackage(path string) (*PkgMeta, error) {
	kind, err := DetectPackage(path)
	if err != nil {
		return nil, err
	}
	switch kind {
	case "ipk":
		return ParseIPK(path)
	case "apk3":
		return ParseAPK3(path)
	}
	return ParseAPK(path)
}

/* ---------- 版本与架构 ---------- */

func splitEpoch(v string) (int64, string) {
	idx := strings.Index(v, ":")
	if idx <= 0 {
		return 0, v
	}
	n, err := strconv.ParseInt(v[:idx], 10, 64)
	if err != nil {
		return 0, v
	}
	return n, v[idx+1:]
}

func splitRevision(v string) (string, string) {
	i := strings.LastIndex(v, "-")
	if i < 0 {
		return v, ""
	}
	rev := v[i+1:]
	if rev == "" {
		return v, ""
	}
	if rev[0] == 'r' {
		rev = rev[1:]
	}
	if rev == "" {
		return v, ""
	}
	for _, r := range rev {
		if r < '0' || r > '9' {
			return v, ""
		}
	}
	return v[:i], rev
}

func versionTokens(v string) []string {
	return strings.FieldsFunc(v, func(r rune) bool {
		return r == '.' || r == '_' || r == '+' || r == '-' || r == '~'
	})
}

func cmpToken(a, b string) int {
	an, aerr := strconv.ParseInt(a, 10, 64)
	bn, berr := strconv.ParseInt(b, 10, 64)
	switch {
	case aerr == nil && berr == nil:
		if an < bn {
			return -1
		}
		if an > bn {
			return 1
		}
		return 0
	case aerr == nil:
		return 1
	case berr == nil:
		return -1
	}
	al, bl := strings.ToLower(a), strings.ToLower(b)
	if al < bl {
		return -1
	}
	if al > bl {
		return 1
	}
	return 0
}

// compareVersions 返回 -1 / 0 / 1，兼容 opkg 与 apk 的常见写法。
func compareVersions(a, b string) int {
	ae, av := splitEpoch(strings.TrimSpace(a))
	be, bv := splitEpoch(strings.TrimSpace(b))
	if ae != be {
		if ae < be {
			return -1
		}
		return 1
	}
	am, ar := splitRevision(av)
	bm, br := splitRevision(bv)
	at, bt := versionTokens(am), versionTokens(bm)
	for i := 0; i < len(at) || i < len(bt); i++ {
		if i >= len(at) {
			if len(bt[i:]) > 0 {
				return -1
			}
			break
		}
		if i >= len(bt) {
			return 1
		}
		if c := cmpToken(at[i], bt[i]); c != 0 {
			return c
		}
	}
	return cmpToken(ar, br)
}

func versionSatisfies(version, op, want string) bool {
	if op == "" || want == "" {
		return true
	}
	c := compareVersions(version, want)
	switch op {
	case "=", "==":
		return c == 0
	case ">":
		return c > 0
	case ">=":
		return c >= 0
	case "<":
		return c < 0
	case "<=", "<<":
		return c <= 0
	case "~", "~=":
		return strings.HasPrefix(version, want)
	}
	return true
}

func archMatches(pkgArch, devArch string) bool {
	pkgArch = strings.TrimSpace(pkgArch)
	if pkgArch == "" || pkgArch == "all" || pkgArch == "noarch" || pkgArch == "*" {
		return true
	}
	return pkgArch == devArch
}

// depNameOp 把 `foo>=1.2` / `foo (>= 1.2)` 拆成名称与版本约束。
func depNameOp(dep string) (name, op, ver string) {
	dep = strings.TrimSpace(dep)
	if dep == "" {
		return "", "", ""
	}
	if dep[0] == '!' {
		dep = dep[1:]
	}
	if i := strings.Index(dep, "("); i > 0 {
		name = strings.TrimSpace(dep[:i])
		rest := strings.TrimSuffix(strings.TrimSpace(dep[i+1:]), ")")
		for _, o := range []string{">=", "<=", "<<", ">>", "=", ">", "<"} {
			if strings.HasPrefix(rest, o) {
				return name, o, strings.TrimSpace(strings.TrimPrefix(rest, o))
			}
		}
		return name, "", ""
	}
	for i := 0; i < len(dep); i++ {
		c := dep[i]
		if c == '=' || c == '<' || c == '>' || c == '~' {
			j := i + 1
			if j < len(dep) && (dep[j] == '=' || dep[j] == '>') {
				j++
			}
			return strings.TrimSpace(dep[:i]), dep[i:j], strings.TrimSpace(dep[j:])
		}
	}
	return dep, "", ""
}
