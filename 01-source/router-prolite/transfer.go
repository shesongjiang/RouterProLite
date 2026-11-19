package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"
)

func (r *Router) sftpClient() (*sftp.Client, uint64, error) {
	client, err := r.ensureClient()
	if err != nil {
		r.mu.Lock()
		gen := r.gen
		r.mu.Unlock()
		return nil, gen, err
	}
	r.mu.Lock()
	gen := r.gen
	r.mu.Unlock()
	c, err := sftp.NewClient(client)
	if err != nil {
		return nil, gen, err
	}
	return c, gen, nil
}

// uploadPlan 把计划中的本地包上传到设备 /tmp，并做远端大小与 SHA256 校验。
func (r *Router) uploadPlan(ctx context.Context, plan *InstallPlan, jobID string,
	onFile func(name, status string, sent, total int64), logf func(string)) (string, error) {

	remoteDir := "/tmp/prolite-" + jobID + "/pkgs"
	out, err := r.execJob(ctx, "mkdir -p "+shq(remoteDir), 15*time.Second, nil)
	if err != nil || out.ExitCode != 0 {
		return "", fmt.Errorf("创建远端临时目录失败：%s", strings.TrimSpace(out.Output))
	}

	var sftpCli *sftp.Client
	if c, gen, err := r.sftpClient(); err == nil {
		sftpCli = c
		defer c.Close()
		r.mu.Lock()
		still := r.gen == gen
		r.mu.Unlock()
		if !still {
			return "", errNotConnected
		}
	} else if logf != nil {
		logf("SFTP 不可用，改用 cat 流式上传：" + err.Error())
	}

	for _, it := range plan.Packages {
		if it.Installed {
			if onFile != nil {
				onFile(it.Name, "ok", it.Size, it.Size)
			}
			continue
		}
		local := it.Downloaded
		if local == "" {
			local = it.LocalPath
		}
		if local == "" {
			return "", fmt.Errorf("%s 没有本地文件", it.Name)
		}
		remote := remoteDir + "/" + sanitizePkgFilename(filepath.Base(local))
		if onFile != nil {
			onFile(it.Name, "uploading", 0, it.Size)
		}
		if sftpCli != nil {
			err = uploadSFTP(ctx, sftpCli, local, remote, func(sent int64) {
				if onFile != nil {
					onFile(it.Name, "uploading", sent, it.Size)
				}
			})
		} else {
			err = r.putFileViaCat(ctx, local, remote, func(sent int64) {
				if onFile != nil {
					onFile(it.Name, "uploading", sent, it.Size)
				}
			})
		}
		if err != nil {
			if onFile != nil {
				onFile(it.Name, "failed", 0, it.Size)
			}
			return "", fmt.Errorf("上传 %s 失败：%w", it.Name, err)
		}
		it.RemotePath = remote
		if err := r.verifyRemoteFile(ctx, local, remote); err != nil {
			if onFile != nil {
				onFile(it.Name, "failed", it.Size, it.Size)
			}
			return "", fmt.Errorf("%s 远端校验失败：%w", it.Name, err)
		}
		it.Status = "uploaded"
		if onFile != nil {
			onFile(it.Name, "uploaded", it.Size, it.Size)
		}
		if logf != nil {
			logf("已上传 " + it.Name + " → " + remote)
		}
	}
	return remoteDir, nil
}

func uploadSFTP(ctx context.Context, c *sftp.Client, local, remote string, onProgress func(int64)) error {
	src, err := os.Open(local)
	if err != nil {
		return err
	}
	defer src.Close()
	st, err := src.Stat()
	if err != nil {
		return err
	}
	part := remote + ".part"
	_ = c.Remove(part)
	dst, err := c.Create(part)
	if err != nil {
		return err
	}
	cr := &countReader{r: src, cb: onProgress, every: 200 * time.Millisecond}
	_, copyErr := io.Copy(dst, cr)
	closeErr := dst.Close()
	if copyErr != nil {
		_ = c.Remove(part)
		return copyErr
	}
	if closeErr != nil {
		_ = c.Remove(part)
		return closeErr
	}
	if onProgress != nil {
		onProgress(st.Size())
	}
	if err := c.Rename(part, remote); err != nil {
		_ = c.Remove(part)
		return err
	}
	_ = ctx
	return nil
}

func (r *Router) verifyRemoteFile(ctx context.Context, local, remote string) error {
	st, err := os.Stat(local)
	if err != nil {
		return err
	}
	out, err := r.execJob(ctx, "sha256sum "+shq(remote), 20*time.Second, nil)
	if err == nil && out.ExitCode == 0 {
		f := strings.Fields(out.Output)
		if len(f) > 0 && strings.EqualFold(f[0], sha256File(local)) {
			return nil
		}
		return fmt.Errorf("SHA256 不一致")
	}
	// 没有 sha256sum 时退回大小校验
	sizeOut, serr := r.execJob(ctx, "wc -c < "+shq(remote), 15*time.Second, nil)
	if serr != nil || sizeOut.ExitCode != 0 {
		return fmt.Errorf("无法读取远端文件信息")
	}
	if strings.TrimSpace(sizeOut.Output) != fmt.Sprintf("%d", st.Size()) {
		return fmt.Errorf("远端大小不一致")
	}
	return nil
}

func (r *Router) cleanupRemoteDir(ctx context.Context, jobID string) {
	_, _ = r.execJob(ctx, "rm -rf "+shq("/tmp/prolite-"+jobID), 15*time.Second, nil)
}
