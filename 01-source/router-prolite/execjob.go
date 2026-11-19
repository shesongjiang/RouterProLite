package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// JobOutput 是长任务的执行结果。
type JobOutput struct {
	ExitCode int
	Output   string
	TimedOut bool
}

type lineWriter struct {
	mu     sync.Mutex
	buf    strings.Builder
	cur    strings.Builder
	onLine func(string)
	total  int
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, b := range p {
		if b == '\n' {
			line := strings.TrimRight(w.cur.String(), "\r")
			w.cur.Reset()
			if line != "" && w.onLine != nil {
				w.onLine(line)
			}
			if w.total < maxOutput {
				w.buf.WriteString(line)
				w.buf.WriteByte('\n')
				w.total += len(line) + 1
			}
			continue
		}
		w.cur.WriteByte(b)
	}
	return len(p), nil
}

func (w *lineWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := w.buf.String()
	if w.cur.Len() > 0 {
		out += w.cur.String()
	}
	return out
}

// execJob 是可取消、可超时的长任务通道；不改变 ExecCommand 的现有行为。
func (r *Router) execJob(ctx context.Context, cmd string, timeout time.Duration, onLine func(string)) (JobOutput, error) {
	client := r.currentClient()
	if client == nil {
		return JobOutput{ExitCode: -1}, errNotConnected
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	sess, err := client.NewSession()
	if err != nil {
		return JobOutput{ExitCode: -1}, err
	}
	defer sess.Close()

	lw := &lineWriter{onLine: onLine}
	sess.Stdout = lw
	sess.Stderr = lw
	if err := sess.Start(cmd); err != nil {
		return JobOutput{ExitCode: -1}, err
	}

	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()

	select {
	case werr := <-done:
		out := JobOutput{Output: lw.String()}
		if werr != nil {
			var exit *ssh.ExitError
			if errors.As(werr, &exit) {
				out.ExitCode = exit.ExitStatus()
				return out, nil
			}
			out.ExitCode = -1
			return out, werr
		}
		return out, nil
	case <-ctx.Done():
		_ = sess.Signal(ssh.SIGINT)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = sess.Close()
		}
		return JobOutput{ExitCode: -1, Output: lw.String(), TimedOut: true}, ctx.Err()
	}
}

type countReader struct {
	r     io.Reader
	n     int64
	cb    func(int64)
	last  time.Time
	every time.Duration
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.n += int64(n)
		if c.cb != nil && (c.every == 0 || time.Since(c.last) > c.every) {
			c.last = time.Now()
			c.cb(c.n)
		}
	}
	return n, err
}

// execStreamStdin 通过 SSH stdin 把二进制写入远端命令（cat > file 的回退通道）。
func (r *Router) execStreamStdin(ctx context.Context, cmd string, src io.Reader, total int64,
	onProgress func(int64)) (string, error) {

	client := r.currentClient()
	if client == nil {
		return "", errNotConnected
	}
	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()

	stdin, err := sess.StdinPipe()
	if err != nil {
		return "", err
	}
	var out strings.Builder
	var mu sync.Mutex
	lw := &lineWriter{onLine: func(line string) {
		mu.Lock()
		if out.Len() < maxOutput {
			out.WriteString(line)
			out.WriteByte('\n')
		}
		mu.Unlock()
	}}
	sess.Stdout = lw
	sess.Stderr = lw
	if err := sess.Start(cmd); err != nil {
		return "", err
	}

	copyDone := make(chan error, 1)
	cr := &countReader{r: src, cb: onProgress, every: 200 * time.Millisecond}
	go func() {
		_, cerr := io.Copy(stdin, cr)
		_ = stdin.Close()
		copyDone <- cerr
	}()

	waitDone := make(chan error, 1)
	go func() { waitDone <- sess.Wait() }()

	select {
	case cerr := <-copyDone:
		if cerr != nil {
			_ = sess.Close()
			return out.String(), cerr
		}
	case <-ctx.Done():
		_ = sess.Close()
		return out.String(), ctx.Err()
	}

	select {
	case werr := <-waitDone:
		if onProgress != nil {
			onProgress(cr.n)
		}
		if werr != nil {
			var exit *ssh.ExitError
			if errors.As(werr, &exit) {
				return out.String(), fmt.Errorf("远端命令退出码 %d", exit.ExitStatus())
			}
			return out.String(), werr
		}
		return out.String(), nil
	case <-ctx.Done():
		_ = sess.Close()
		return out.String(), ctx.Err()
	case <-time.After(30 * time.Second):
		_ = sess.Close()
		return out.String(), fmt.Errorf("等待远端写入完成超时")
	}
}

// putFileViaCat 是无 sftp-server 时的上传回退。
func (r *Router) putFileViaCat(ctx context.Context, local, remote string, onProgress func(int64)) error {
	f, err := os.Open(local)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	cmd := "cat > " + shq(remote+".part") + " && mv " + shq(remote+".part") + " " + shq(remote)
	_, err = r.execStreamStdin(ctx, cmd, f, st.Size(), onProgress)
	return err
}
