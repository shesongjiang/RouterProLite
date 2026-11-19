package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// IndexPkg 是索引中的一条包记录。
type IndexPkg struct {
	Name          string
	Version       string
	Arch          string
	Size          int64
	InstalledSize int64
	Description   string
	License       string
	Section       string
	Origin        string
	Homepage      string
	Depends       []string
	Provides      []string
	Conflicts     []string
	Replaces      []string
	Filename      string
	URL           string
	Feed          string
	Checksum      string
	ChecksumKind  string
	Installed     bool
}

// Universe 是全部索引与设备已装包的集合。
type Universe struct {
	Pkgs      []*IndexPkg
	ByName    map[string][]*IndexPkg
	Providers map[string][]*IndexPkg
	Installed map[string]InstalledPkg
	Env       *PkgEnv
}

func (u *Universe) add(p *IndexPkg) {
	if p == nil || p.Name == "" {
		return
	}
	u.Pkgs = append(u.Pkgs, p)
	u.ByName[p.Name] = append(u.ByName[p.Name], p)
	for _, prov := range p.Provides {
		n, _, _ := depNameOp(prov)
		if n != "" {
			u.Providers[n] = append(u.Providers[n], p)
		}
	}
}

func (u *Universe) finalize() {
	for _, list := range u.ByName {
		sort.SliceStable(list, func(i, j int) bool {
			return compareVersions(list[i].Version, list[j].Version) > 0
		})
	}
	for _, list := range u.Providers {
		sort.SliceStable(list, func(i, j int) bool {
			return compareVersions(list[i].Version, list[j].Version) > 0
		})
	}
}

// FetchUniverse 拉取设备配置的全部 feed 索引（桌面端在线，设备端不联网）。
func (r *Router) FetchUniverse(env *PkgEnv, logf func(string)) (*Universe, []Check, error) {
	u := &Universe{
		ByName:    map[string][]*IndexPkg{},
		Providers: map[string][]*IndexPkg{},
		Installed: env.Installed,
		Env:       env,
	}
	checks := []Check{}
	if len(env.Feeds) == 0 {
		return nil, checks, fmt.Errorf("设备没有可用的包源配置")
	}
	if len(env.Feeds) > 8 {
		env.Feeds = env.Feeds[:8]
		checks = append(checks, Check{ID: "IDX-LIMIT", Stage: "index", Label: "包源数量", Level: "warn", Detail: "仅使用前 8 个源"})
	}

	for i, feed := range env.Feeds {
		id := fmt.Sprintf("IDX-%d", i+1)
		start := time.Now()
		if logf != nil {
			logf("拉取索引 " + feed.URL)
		}
		var entries []*IndexPkg
		var err error
		verified := false
		note := ""
		if env.Manager == "opkg" {
			entries, err = r.fetchOpkgIndex(feed, 6*time.Hour)
			note = "opkg 索引签名未在 M1 校验（下载包时校验 SHA256，安装时由 opkg 验签）"
		} else {
			entries, verified, note, err = r.fetchAnyIndexWithKeys(feed, env.ApkKeys, 6*time.Hour)
		}
		if err != nil {
			checks = append(checks, Check{
				ID: id, Stage: "index", Label: feed.Name,
				Level: "warn", Detail: fmt.Sprintf("%s 不可用：%s", feed.URL, err.Error()),
			})
			if logf != nil {
				logf("索引失败 " + feed.URL + "：" + err.Error())
			}
			continue
		}
		for _, p := range entries {
			u.add(p)
		}
		level := "ok"
		detail := fmt.Sprintf("%d 个包，用时 %s", len(entries), time.Since(start).Round(time.Millisecond))
		if note != "" {
			detail += "；" + note
		}
		if env.Manager == "apk" && !verified {
			level = "warn"
			if note == "" {
				detail += "；索引签名未验证"
			}
		}
		checks = append(checks, Check{ID: id, Stage: "index", Label: feed.Name, Level: level, Detail: detail})
	}

	if len(u.Pkgs) == 0 {
		return nil, checks, fmt.Errorf("所有索引均不可用，无法在桌面端解析依赖")
	}
	u.finalize()
	return u, checks, nil
}

func (r *Router) fetchAPKIndex(feed Feed, keys map[string]string, maxAge time.Duration) ([]*IndexPkg, bool, string, error) {
	url := strings.TrimRight(feed.URL, "/") + "/APKINDEX.tar.gz"
	data, err := fetchWithCache(url, "APKINDEX.tar.gz", maxAge)
	if err != nil {
		return nil, false, "", err
	}
	index, sigs, err := parseAPKIndexTar(data)
	if err != nil {
		return nil, false, "", err
	}
	verified, note := verifyAPKIndex(index, sigs, keys)
	return parseAPKIndexRecords(string(index), feed), verified, note, nil
}

func (r *Router) fetchOpkgIndex(feed Feed, maxAge time.Duration) ([]*IndexPkg, error) {
	base := strings.TrimRight(feed.URL, "/")
	data, err := fetchWithCache(base+"/Packages.gz", "Packages.gz", maxAge)
	if err != nil {
		data, err = fetchWithCache(base+"/Packages", "Packages", maxAge)
		if err != nil {
			return nil, err
		}
		return parseOpkgPackages(string(data), feed)
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("Packages.gz 解压失败：%w", err)
	}
	defer gz.Close()
	raw, err := io.ReadAll(io.LimitReader(gz, 32*1024*1024))
	if err != nil {
		return nil, err
	}
	return parseOpkgPackages(string(raw), feed)
}

// fetchAnyIndex 用于插件市场：先试 apk v3 packages.adb，再回退 APKINDEX.tar.gz / Packages.gz。
func (r *Router) fetchAnyIndex(feed Feed, maxAge time.Duration) ([]*IndexPkg, string, error) {
	entries, _, note, err := r.fetchAnyIndexWithKeys(feed, nil, maxAge)
	return entries, note, err
}

// fetchAnyIndexWithKeys 在 apk 分支上保留旧 APKINDEX 的 RSA/签名校验能力。
func (r *Router) fetchAnyIndexWithKeys(feed Feed, keys map[string]string,
	maxAge time.Duration) ([]*IndexPkg, bool, string, error) {

	adbURL, base, hasADB := adbIndexURL(feed.URL)
	baseFeed := Feed{Name: feed.Name, URL: base}
	var adbErr error
	if hasADB {
		data, err := fetchWithCache(adbURL, "packages.adb", maxAge)
		if err == nil {
			entries, note, perr := parseADBIndex(data, baseFeed)
			if perr == nil && len(entries) > 0 {
				return entries, false, note, nil
			}
			adbErr = perr
		} else {
			adbErr = err
		}
	}

	var entries []*IndexPkg
	var verified bool
	var note string
	var terr error
	if base != "" {
		entries, verified, note, terr = r.fetchAPKIndex(baseFeed, keys, maxAge)
	} else {
		terr = fmt.Errorf("无法从 %s 推导仓库根目录", feed.URL)
	}
	if terr == nil && len(entries) > 0 {
		if adbErr != nil {
			note = "packages.adb 不可用（" + adbErr.Error() + "），已回退 APKINDEX.tar.gz；" + note
		}
		return entries, verified, note, nil
	}
	var opkgEntries []*IndexPkg
	var oerr error
	if base != "" {
		opkgEntries, oerr = r.fetchOpkgIndex(baseFeed, maxAge)
	} else {
		oerr = terr
	}
	if oerr == nil && len(opkgEntries) > 0 {
		if adbErr != nil {
			return opkgEntries, false, "packages.adb 不可用（" + adbErr.Error() + "），已回退 opkg Packages.gz", nil
		}
		return opkgEntries, false, "opkg 索引（签名由 opkg 在设备端校验）", nil
	}
	if adbErr == nil && hasADB {
		adbErr = fmt.Errorf("未解析到包记录")
	}
	if terr == nil {
		terr = fmt.Errorf("未解析到包记录")
	}
	if oerr == nil {
		oerr = fmt.Errorf("未解析到包记录")
	}
	return nil, false, "", fmt.Errorf("packages.adb：%v；APKINDEX.tar.gz：%v；Packages.gz：%v", adbErr, terr, oerr)
}

func parseAPKIndexTar(data []byte) ([]byte, map[string][]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, nil, fmt.Errorf("APKINDEX.tar.gz 解压失败：%w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var index []byte
	sigs := map[string][]byte{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("APKINDEX 读取失败：%w", err)
		}
		name := strings.TrimPrefix(h.Name, "./")
		body, err := io.ReadAll(io.LimitReader(tr, 64*1024*1024))
		if err != nil {
			return nil, nil, err
		}
		if name == "APKINDEX" {
			index = body
		} else if strings.HasPrefix(name, ".SIGN.") {
			sigs[name] = body
		}
	}
	if len(index) == 0 {
		return nil, nil, fmt.Errorf("APKINDEX.tar.gz 中未找到 APKINDEX")
	}
	return index, sigs, nil
}

func verifyAPKIndex(index []byte, sigs map[string][]byte, keys map[string]string) (bool, string) {
	if len(sigs) == 0 {
		return false, "索引无签名成员"
	}
	if len(keys) == 0 {
		return false, "设备未提供 /etc/apk/keys 公钥"
	}
	for name, sig := range sigs {
		rest := strings.TrimPrefix(name, ".SIGN.")
		alg := ""
		keyName := ""
		switch {
		case strings.HasPrefix(rest, "RSA256."):
			alg, keyName = "sha256", strings.TrimPrefix(rest, "RSA256.")
		case strings.HasPrefix(rest, "RSA."):
			alg, keyName = "sha1", strings.TrimPrefix(rest, "RSA.")
		default:
			continue
		}
		pemText, ok := lookupKey(keys, keyName)
		if !ok {
			continue
		}
		pub, err := parseRSAPublicKey(pemText)
		if err != nil {
			continue
		}
		var h crypto.Hash
		var digest []byte
		if alg == "sha256" {
			h = crypto.SHA256
			sum := sha256.Sum256(index)
			digest = sum[:]
		} else {
			h = crypto.SHA1
			sum := sha1.Sum(index)
			digest = sum[:]
		}
		if err := rsa.VerifyPKCS1v15(pub, h, digest, sig); err == nil {
			return true, ""
		}
	}
	return false, "索引签名无法用设备公钥验证"
}

func lookupKey(keys map[string]string, name string) (string, bool) {
	if v, ok := keys[name]; ok {
		return v, true
	}
	for k, v := range keys {
		if strings.TrimSuffix(k, ".rsa.pub") == name || strings.HasPrefix(k, name) {
			return v, true
		}
	}
	return "", false
}

func parseRSAPublicKey(text string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(text))
	if block == nil {
		return nil, fmt.Errorf("公钥不是 PEM")
	}
	if strings.Contains(block.Type, "RSA PUBLIC KEY") {
		return x509.ParsePKCS1PublicKey(block.Bytes)
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rp, ok := key.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("不是 RSA 公钥")
	}
	return rp, nil
}

func parseAPKIndexRecords(raw string, feed Feed) []*IndexPkg {
	out := []*IndexPkg{}
	for _, stanza := range strings.Split(raw, "\n\n") {
		stanza = strings.TrimSpace(stanza)
		if stanza == "" {
			continue
		}
		m := map[string]string{}
		for _, line := range strings.Split(stanza, "\n") {
			if len(line) < 2 || line[1] != ':' {
				continue
			}
			m[line[:1]] = strings.TrimSpace(line[2:])
		}
		if m["P"] == "" || m["V"] == "" {
			continue
		}
		size, _ := strconv.ParseInt(m["S"], 10, 64)
		installed, _ := strconv.ParseInt(m["I"], 10, 64)
		base := strings.TrimRight(feed.URL, "/")
		out = append(out, &IndexPkg{
			Name:          m["P"],
			Version:       m["V"],
			Arch:          m["A"],
			Size:          size,
			InstalledSize: installed,
			Description:   m["T"],
			License:       m["L"],
			Origin:        m["o"],
			Homepage:      m["U"],
			Depends:       splitList(m["D"]),
			Provides:      splitList(m["p"]),
			Replaces:      splitList(m["r"]),
			Checksum:      m["C"],
			Feed:          base,
			Filename:      m["P"] + "-" + m["V"] + ".apk",
			URL:           base + "/" + m["P"] + "-" + m["V"] + ".apk",
		})
	}
	return out
}

func parseOpkgPackages(raw string, feed Feed) ([]*IndexPkg, error) {
	out := []*IndexPkg{}
	base := strings.TrimRight(feed.URL, "/")
	var cur map[string]string
	flush := func() {
		if cur == nil {
			return
		}
		if cur["package"] != "" && cur["version"] != "" {
			size, _ := strconv.ParseInt(cur["size"], 10, 64)
			installed, _ := strconv.ParseInt(cur["installed-size"], 10, 64)
			fn := strings.TrimPrefix(cur["filename"], "./")
			out = append(out, &IndexPkg{
				Name:          cur["package"],
				Version:       cur["version"],
				Arch:          cur["architecture"],
				Size:          size,
				InstalledSize: installed,
				Description:   cur["description"],
				License:       cur["license"],
				Section:       cur["section"],
				Origin:        cur["source"],
				Homepage:      cur["homepage"],
				Depends:       splitCommaList(cur["depends"]),
				Provides:      splitCommaList(cur["provides"]),
				Conflicts:     splitCommaList(cur["conflicts"]),
				Replaces:      splitCommaList(cur["replaces"]),
				Checksum:      cur["sha256sum"],
				Filename:      fn,
				URL:           base + "/" + fn,
				Feed:          base,
			})
		}
		cur = nil
	}
	sc := bufio.NewScanner(strings.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var lastKey string
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			flush()
			lastKey = ""
			continue
		}
		if cur == nil {
			cur = map[string]string{}
		}
		if line[0] == ' ' || line[0] == '\t' {
			if lastKey != "" {
				cur[lastKey] += " " + strings.TrimSpace(line)
			}
			continue
		}
		idx := strings.Index(line, ":")
		if idx <= 0 {
			continue
		}
		lastKey = strings.ToLower(strings.TrimSpace(line[:idx]))
		cur[lastKey] = strings.TrimSpace(line[idx+1:])
	}
	flush()
	return out, nil
}

/* ---------- HTTP 缓存 ---------- */

func cacheRoot() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	return filepath.Join(base, "ProLite", "cache")
}

func fetchWithCache(url, cacheName string, maxAge time.Duration) ([]byte, error) {
	dir := filepath.Join(cacheRoot(), "feeds")
	_ = os.MkdirAll(dir, 0o700)
	sum := sha1.Sum([]byte(url))
	file := filepath.Join(dir, hex.EncodeToString(sum[:])+"_"+cacheName)
	if maxAge > 0 {
		if st, err := os.Stat(file); err == nil && time.Since(st.ModTime()) < maxAge {
			if b, err := os.ReadFile(file); err == nil && len(b) > 0 {
				return b, nil
			}
		}
	}
	b, err := httpGet(url, 60*time.Second)
	if err != nil {
		if old, rerr := os.ReadFile(file); rerr == nil && len(old) > 0 && maxAge > 0 {
			return old, nil
		}
		return nil, err
	}
	_ = os.WriteFile(file, b, 0o600)
	return b, nil
}

func httpGet(url string, timeout time.Duration) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ProLite/"+appVersion)
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		reason := strings.TrimSpace(string(body))
		reason = strings.Join(strings.Fields(reason), " ")
		if len(reason) > 160 {
			reason = reason[:160]
		}
		if reason == "" {
			reason = http.StatusText(resp.StatusCode)
		}
		return nil, fmt.Errorf("HTTP %d %s：%s", resp.StatusCode, http.StatusText(resp.StatusCode), reason)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64*1024*1024))
}

func pkgCacheDir() string {
	d := filepath.Join(cacheRoot(), "packages")
	_ = os.MkdirAll(d, 0o700)
	return d
}

func sanitizePkgFilename(name string) string {
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '.' || r == '-' || r == '_' || r == '+' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "package"
	}
	return b.String()
}
