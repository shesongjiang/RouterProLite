package main

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DownloadPlan 把计划中的远端包下载到本地缓存，带并发、续传与 SHA256 校验。
func DownloadPlan(ctx context.Context, plan *InstallPlan, logf func(string),
	onFile func(name, status string, sent, total int64)) error {

	type task struct {
		item *PlanItem
	}
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	setErr := func(err error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		mu.Unlock()
	}

	for _, it := range plan.Packages {
		if it.Installed {
			if onFile != nil {
				onFile(it.Name, "ok", it.Size, it.Size)
			}
			continue
		}
		if it.LocalPath != "" {
			if onFile != nil {
				onFile(it.Name, "ok", it.Size, it.Size)
			}
			continue
		}
		it := it
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				setErr(ctx.Err())
				return
			}
			if err := downloadOne(ctx, it, logf, onFile); err != nil {
				setErr(err)
			}
		}()
	}
	wg.Wait()
	return firstErr
}

func downloadOne(ctx context.Context, it *PlanItem, logf func(string),
	onFile func(name, status string, sent, total int64)) error {

	if it.URL == "" {
		return fmt.Errorf("%s 没有下载地址", it.Name)
	}
	dir := pkgCacheDir()
	name := sanitizePkgFilename(it.Filename)
	if name == "" || name == "package" {
		name = sanitizePkgFilename(it.Name + "-" + it.Version + ".pkg")
	}
	dest := filepath.Join(dir, name)
	part := dest + ".part"

	if st, err := os.Stat(dest); err == nil && st.Size() > 0 {
		if it.Size > 0 && st.Size() != it.Size {
			_ = os.Remove(dest)
		} else if err := verifyFileChecksum(dest, it); err == nil {
			it.Downloaded = dest
			it.SHA256, _ = fileSHA256(dest)
			if logf != nil {
				logf("缓存命中 " + it.Name + " " + it.Version)
			}
			if onFile != nil {
				onFile(it.Name, "ok", st.Size(), st.Size())
			}
			return nil
		}
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		if onFile != nil {
			onFile(it.Name, "downloading", 0, it.Size)
		}
		n, err := downloadToFile(ctx, it.URL, part, it.Size, func(sent int64) {
			if onFile != nil {
				onFile(it.Name, "downloading", sent, it.Size)
			}
		})
		if err != nil {
			lastErr = err
			if logf != nil {
				logf(fmt.Sprintf("下载失败 %s（第 %d 次）：%s", it.Name, attempt+1, err))
			}
			continue
		}
		if it.Size > 0 && n != it.Size {
			lastErr = fmt.Errorf("大小不符：期望 %d，实际 %d", it.Size, n)
			_ = os.Remove(part)
			continue
		}
		if err := os.Rename(part, dest); err != nil {
			lastErr = err
			continue
		}
		if err := verifyFileChecksum(dest, it); err != nil {
			lastErr = err
			_ = os.Remove(dest)
			continue
		}
		it.Downloaded = dest
		it.SHA256, _ = fileSHA256(dest)
		if onFile != nil {
			onFile(it.Name, "ok", n, n)
		}
		if logf != nil {
			logf("下载完成 " + it.Name + " " + it.Version + "（" + humanBytes(n) + "）")
		}
		return nil
	}
	if onFile != nil {
		onFile(it.Name, "failed", 0, it.Size)
	}
	return fmt.Errorf("下载 %s 失败：%w", it.Name, lastErr)
}

func downloadToFile(ctx context.Context, url, dest string, total int64, progress func(int64)) (int64, error) {
	var offset int64
	if st, err := os.Stat(dest); err == nil {
		offset = st.Size()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return offset, err
	}
	req.Header.Set("User-Agent", "ProLite/"+appVersion)
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	client := &http.Client{Timeout: 300 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return offset, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		_ = os.Remove(dest)
		return 0, fmt.Errorf("续传位置失效")
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return offset, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	mode := os.O_CREATE | os.O_WRONLY
	if offset > 0 && resp.StatusCode == http.StatusPartialContent {
		mode |= os.O_APPEND
	} else {
		offset = 0
		mode |= os.O_TRUNC
	}
	f, err := os.OpenFile(dest, mode, 0o600)
	if err != nil {
		return offset, err
	}
	defer f.Close()

	buf := make([]byte, 64*1024)
	n := offset
	lastReport := time.Now()
	for {
		select {
		case <-ctx.Done():
			return n, ctx.Err()
		default:
		}
		m, rerr := resp.Body.Read(buf)
		if m > 0 {
			if _, werr := f.Write(buf[:m]); werr != nil {
				return n, werr
			}
			n += int64(m)
			if progress != nil && time.Since(lastReport) > 200*time.Millisecond {
				progress(n)
				lastReport = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return n, rerr
		}
	}
	if progress != nil {
		progress(n)
	}
	return n, nil
}

func verifyFileChecksum(path string, it *PlanItem) error {
	sum, err := fileSHA256(path)
	if err != nil {
		return err
	}
	it.SHA256 = sum
	c := strings.TrimSpace(it.Checksum)
	if c == "" {
		return nil
	}
	if it.ChecksumKind == "adb-payload" {
		adbSum, err := adbPackagePayloadSHA256(path)
		if err != nil {
			return fmt.Errorf("ADB 元数据块校验失败：%w", err)
		}
		if strings.EqualFold(c, adbSum) {
			return nil
		}
		return fmt.Errorf("ADB 元数据块 SHA256 不匹配")
	}
	if len(c) == 64 && isHex(c) {
		if strings.EqualFold(c, sum) {
			return nil
		}
		return fmt.Errorf("SHA256 不匹配")
	}
	if strings.HasPrefix(c, "Q1") {
		raw, err := base64.StdEncoding.DecodeString(c[2:])
		if err == nil && len(raw) == sha1.Size {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			h := sha1.New()
			if _, err := io.Copy(h, f); err != nil {
				return err
			}
			if hex.EncodeToString(h.Sum(nil)) == hex.EncodeToString(raw) {
				return nil
			}
			return fmt.Errorf("SHA1 不匹配")
		}
	}
	if strings.HasPrefix(strings.ToUpper(c), "SHA256:") {
		if strings.EqualFold(c[7:], sum) {
			return nil
		}
		return fmt.Errorf("SHA256 不匹配")
	}
	return nil
}

func isHex(s string) bool {
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func sha256File(path string) string {
	h := sha256.New()
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	_, _ = io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil))
}
