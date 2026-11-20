package main

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/ssh"
)

type Router struct {
	lifecycleMu sync.Mutex
	refreshMu   sync.Mutex
	mu          sync.Mutex

	client        *ssh.Client
	input         *ConnectionInput // 会话级凭据：仅内存，不写盘
	pinned        string
	credExpired   bool // 保存的口令已失效，等待用户重新输入
	info          *ConnectionInfo
	gen           uint64
	meta          *Meta
	metaAt        time.Time
	snap          *Snapshot
	snapAt        time.Time
	console       *ConsoleData
	consoleAt     time.Time
	lastErr       string
	lastReconnect time.Time

	prevCPU     cpuStat
	prevCPUTime time.Time
	prevNet     map[string]netCounter
	prevNetTime time.Time

	latencyMs   float64
	latencyOK   bool
	stopLatency chan struct{}

	// dial 仅用于测试注入；正常运行时走 dialSSH。
	dial func(ConnectionInput, string) (*ssh.Client, string, error)
}

func newRouter() *Router {
	return &Router{
		prevNet: map[string]netCounter{},
	}
}

func (r *Router) dialNow(in ConnectionInput, pinned string) (*ssh.Client, string, error) {
	if r.dial != nil {
		return r.dial(in, pinned)
	}
	return dialSSH(in, pinned)
}

func (r *Router) Connect(in ConnectionInput, pinned string) (*ConnectionInfo, error) {
	normalized, err := normalizeConnection(in)
	if err != nil {
		return nil, err
	}
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	client, fp, err := r.dialNow(normalized, pinned)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	if r.stopLatency != nil {
		close(r.stopLatency)
		r.stopLatency = nil
	}
	oldClient := r.client
	r.gen++
	gen := r.gen
	r.client = client
	r.input = &normalized
	r.pinned = fp
	r.credExpired = false
	r.info = nil
	r.meta = nil
	r.metaAt = time.Time{}
	r.snap = nil
	r.snapAt = time.Time{}
	r.console = nil
	r.consoleAt = time.Time{}
	r.prevCPU = cpuStat{}
	r.prevCPUTime = time.Time{}
	r.prevNet = map[string]netCounter{}
	r.prevNetTime = time.Time{}
	r.latencyMs = 0
	r.latencyOK = false
	r.lastErr = ""
	r.lastReconnect = time.Time{}
	stop := make(chan struct{})
	r.stopLatency = stop
	r.mu.Unlock()

	if oldClient != nil {
		_ = oldClient.Close()
	}

	meta, err := r.fetchMeta()
	if err != nil {
		_ = client.Close()
		r.mu.Lock()
		if r.gen == gen {
			r.client = nil
			r.input = nil
			if r.stopLatency == stop {
				close(stop)
				r.stopLatency = nil
			}
		}
		r.mu.Unlock()
		return nil, fmt.Errorf("SSH 已连接，但读取设备信息失败：%w", err)
	}

	info := meta.connectionInfo(normalized, fp)
	r.mu.Lock()
	if r.gen == gen {
		r.info = info
		r.lastErr = ""
	}
	r.mu.Unlock()

	go r.latencyLoop(stop)

	if snap, err := r.collectSnapshot(meta, info); err == nil {
		r.mu.Lock()
		if r.gen == gen {
			r.snap = snap
			r.snapAt = time.Now()
		}
		r.mu.Unlock()
	}
	return info, nil
}

func (r *Router) Disconnect() {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	r.mu.Lock()
	client := r.client
	r.client = nil
	r.gen++
	r.input = nil
	r.credExpired = false
	r.info = nil
	r.meta = nil
	r.metaAt = time.Time{}
	r.snap = nil
	r.snapAt = time.Time{}
	r.console = nil
	r.consoleAt = time.Time{}
	r.lastErr = ""
	if r.stopLatency != nil {
		close(r.stopLatency)
		r.stopLatency = nil
	}
	r.mu.Unlock()
	if client != nil {
		_ = client.Close()
	}
}

func (r *Router) currentClient() *ssh.Client {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.client
}

func (r *Router) currentInfo() *ConnectionInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.info
}

// TargetHost 返回当前连接的目标地址，用于真实写入前的设备级确认。
func (r *Router) TargetHost() string {
	info := r.currentInfo()
	if info == nil {
		return ""
	}
	return info.Host
}

func (r *Router) currentMeta() *Meta {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.meta
}

func (r *Router) exec(cmd string, timeout time.Duration) (string, error) {
	client, err := r.ensureClient()
	if err != nil {
		return "", err
	}
	out, _, err := execWithClient(client, cmd, timeout)
	return out, err
}

func execWithClient(client *ssh.Client, cmd string, timeout time.Duration) (string, int, error) {
	if client == nil {
		return "", -1, errNotConnected
	}
	sess, err := client.NewSession()
	if err != nil {
		return "", -1, err
	}

	type result struct {
		out []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := sess.CombinedOutput(cmd)
		done <- result{out: out, err: err}
	}()

	select {
	case rr := <-done:
		code := 0
		if rr.err != nil {
			code = -1
			var exit *ssh.ExitError
			if errors.As(rr.err, &exit) {
				code = exit.ExitStatus()
			}
		}
		return capOutput(string(rr.out)), code, rr.err
	case <-time.After(timeout):
		_ = sess.Close()
		return "", -1, errors.New("命令执行超时")
	}
}

const maxOutput = 192 * 1024

func capOutput(s string) string {
	if len(s) <= maxOutput {
		return s
	}
	cut := s[:maxOutput]
	// 回退到完整的 UTF-8 边界，避免把多字节中文截成乱码
	for i := 0; i < 4 && len(cut) > 0 && !utf8.ValidString(cut); i++ {
		cut = cut[:len(cut)-1]
	}
	return cut + "\n... (输出已截断)"
}

func (r *Router) fetchMeta() (*Meta, error) {
	r.mu.Lock()
	client := r.client
	gen := r.gen
	r.mu.Unlock()
	if client == nil {
		return nil, errNotConnected
	}
	out, _, err := execWithClient(client, metaScript, 15*time.Second)
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, err
	}
	meta := parseMetaOutput(out)
	if len(meta.Init) == 0 && len(meta.UCI["network"]) == 0 && meta.Board.Release["distribution"] == "" {
		return nil, errors.New("未识别到 OpenWrt 系统（ubus / uci 不可用）")
	}
	r.mu.Lock()
	if r.gen == gen && r.client == client {
		r.meta = meta
		r.metaAt = time.Now()
	}
	r.mu.Unlock()
	return meta, nil
}

func (r *Router) Snapshot() Snapshot {
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()

	r.mu.Lock()
	if r.client == nil {
		r.mu.Unlock()
		return r.disconnectedSnapshot()
	}
	if r.snap != nil && time.Since(r.snapAt) < 700*time.Millisecond {
		s := *r.snap
		r.mu.Unlock()
		return s
	}
	meta := r.meta
	metaAt := r.metaAt
	info := r.info
	r.mu.Unlock()

	if info == nil {
		return r.disconnectedSnapshot()
	}

	if meta == nil || time.Since(metaAt) > 30*time.Second {
		if m, err := r.fetchMeta(); err == nil {
			meta = m
		}
	}
	if meta == nil {
		if !r.tryReconnect() {
			return r.failureSnapshot(errors.New("读取设备状态失败"))
		}
		r.mu.Lock()
		meta = r.meta
		info = r.info
		r.mu.Unlock()
		if meta == nil || info == nil {
			return r.failureSnapshot(errors.New("重连后仍无法读取设备状态"))
		}
	}

	snap, err := r.collectSnapshot(meta, info)
	if err == nil {
		r.mu.Lock()
		r.snap = snap
		r.snapAt = time.Now()
		r.lastErr = ""
		r.mu.Unlock()
		return *snap
	}

	r.markBroken(err)
	if r.tryReconnect() {
		r.mu.Lock()
		meta = r.meta
		info = r.info
		r.mu.Unlock()
		if meta != nil && info != nil {
			if snap2, err2 := r.collectSnapshot(meta, info); err2 == nil {
				r.mu.Lock()
				r.snap = snap2
				r.snapAt = time.Now()
				r.lastErr = ""
				r.mu.Unlock()
				return *snap2
			}
		}
	}
	return r.failureSnapshot(err)
}

func (r *Router) markBroken(err error) {
	r.mu.Lock()
	client := r.client
	r.client = nil
	r.gen++
	r.snap = nil
	r.snapAt = time.Time{}
	r.console = nil
	r.consoleAt = time.Time{}
	if r.stopLatency != nil {
		close(r.stopLatency)
		r.stopLatency = nil
	}
	if err != nil {
		r.lastErr = err.Error()
	}
	r.mu.Unlock()
	if client != nil {
		_ = client.Close()
	}
}

// sessionAvailable 判断当前是否还有可用的会话（连接或内存凭据）。
func (r *Router) sessionAvailable() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.credExpired {
		return &authExpiredError{}
	}
	if r.client == nil && r.input == nil {
		return errNotConnected
	}
	return nil
}

// ensureClient 返回可用连接；如果连接已断开，会用内存里的会话凭据自动重建。
// 插件安装、存储、控制台、日志等所有 SSH 后台操作都走这里。
func (r *Router) ensureClient() (*ssh.Client, error) {
	if c := r.currentClient(); c != nil {
		return c, nil
	}
	r.mu.Lock()
	expired := r.credExpired
	r.mu.Unlock()
	if expired {
		return nil, &authExpiredError{}
	}

	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	if c := r.currentClient(); c != nil {
		return c, nil
	}

	r.mu.Lock()
	in := r.input
	pinned := r.pinned
	r.mu.Unlock()
	if in == nil {
		return nil, errNotConnected
	}

	client, fp, err := r.dialNow(*in, pinned)
	if err != nil {
		var ae *authExpiredError
		if errors.As(err, &ae) {
			r.mu.Lock()
			r.input = nil
			r.credExpired = true
			r.lastErr = ae.Error()
			r.mu.Unlock()
			return nil, ae
		}
		r.mu.Lock()
		r.lastErr = err.Error()
		r.mu.Unlock()
		return nil, err
	}

	r.mu.Lock()
	if r.input == nil {
		r.mu.Unlock()
		_ = client.Close()
		return nil, errNotConnected
	}
	oldClient := r.client
	r.gen++
	r.client = client
	r.pinned = fp
	r.lastErr = ""
	r.snap = nil
	r.snapAt = time.Time{}
	r.console = nil
	r.consoleAt = time.Time{}
	r.prevCPU = cpuStat{}
	r.prevCPUTime = time.Time{}
	r.prevNet = map[string]netCounter{}
	r.prevNetTime = time.Time{}
	if r.stopLatency != nil {
		close(r.stopLatency)
	}
	stop := make(chan struct{})
	r.stopLatency = stop
	r.mu.Unlock()
	if oldClient != nil && oldClient != client {
		_ = oldClient.Close()
	}
	// 自动重连成功后重启延迟探测，否则主面板延迟会一直显示 "--" 直到手动重连
	go r.latencyLoop(stop)
	return client, nil
}

func (r *Router) tryReconnect() bool {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	r.mu.Lock()
	in := r.input
	pinned := r.pinned
	if in == nil || time.Since(r.lastReconnect) < 10*time.Second {
		r.mu.Unlock()
		return false
	}
	r.lastReconnect = time.Now()
	input := *in
	r.mu.Unlock()

	client, fp, err := r.dialNow(input, pinned)
	if err != nil {
		var ae *authExpiredError
		if errors.As(err, &ae) {
			r.mu.Lock()
			r.input = nil
			r.credExpired = true
			r.lastErr = ae.Error()
			r.mu.Unlock()
			return false
		}
		r.mu.Lock()
		r.lastErr = "重连失败：" + err.Error()
		r.mu.Unlock()
		return false
	}

	r.mu.Lock()
	oldClient := r.client
	r.gen++
	gen := r.gen
	r.client = client
	r.pinned = fp
	r.credExpired = false
	r.snap = nil
	r.snapAt = time.Time{}
	r.console = nil
	r.consoleAt = time.Time{}
	r.prevCPU = cpuStat{}
	r.prevCPUTime = time.Time{}
	r.prevNet = map[string]netCounter{}
	r.prevNetTime = time.Time{}
	stop := make(chan struct{})
	r.stopLatency = stop
	r.mu.Unlock()
	if oldClient != nil {
		_ = oldClient.Close()
	}

	out, _, err := execWithClient(client, metaScript, 15*time.Second)
	if err != nil && strings.TrimSpace(out) == "" {
		_ = client.Close()
		r.mu.Lock()
		if r.gen == gen {
			r.client = nil
			if r.stopLatency == stop {
				close(stop)
				r.stopLatency = nil
			}
			r.lastErr = "重连失败：" + err.Error()
		}
		r.mu.Unlock()
		return false
	}
	meta := parseMetaOutput(out)
	r.mu.Lock()
	if r.gen == gen {
		r.meta = meta
		r.metaAt = time.Now()
		r.lastErr = ""
	}
	r.mu.Unlock()
	go r.latencyLoop(stop)
	return true
}

func (r *Router) failureSnapshot(err error) Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.snap != nil {
		s := *r.snap
		s.Connected = r.client != nil
		if err != nil {
			s.Error = err.Error()
		} else if r.lastErr != "" {
			s.Error = r.lastErr
		}
		return s
	}
	return Snapshot{
		Connected:   r.client != nil,
		Error:       r.lastErr,
		RefreshedAt: time.Now().UnixMilli(),
		Clients:     []Client{},
		Tiles:       []Tile{},
	}
}

func (r *Router) latencyLoop(stop chan struct{}) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			r.mu.Lock()
			client := r.client
			snap := r.snap
			current := r.stopLatency == stop
			r.mu.Unlock()
			if !current || client == nil {
				return
			}
			if snap == nil || snap.WAN == nil || !snap.WAN.Up {
				r.mu.Lock()
				r.latencyOK = false
				r.mu.Unlock()
				continue
			}
			gateway := ""
			if snap.WAN != nil {
				gateway = snap.WAN.Gateway
			}
			out, _, err := execWithClient(client, latencyCommand(latencyTargets(gateway)), 6*time.Second)
			ms, ok := parsePingMS(out)
			r.mu.Lock()
			if err == nil && ok {
				r.latencyMs = ms
				r.latencyOK = true
			} else {
				r.latencyOK = false
			}
			r.mu.Unlock()
		}
	}
}

func (r *Router) Invalidate() {
	r.mu.Lock()
	r.metaAt = time.Time{}
	r.snapAt = time.Time{}
	r.consoleAt = time.Time{}
	r.mu.Unlock()
}

func (r *Router) RunCommand(cmd string, timeout time.Duration) (ProbeResult, error) {
	client, err := r.ensureClient()
	if err != nil {
		return ProbeResult{}, err
	}
	start := time.Now()
	out, code, err := execWithClient(client, cmd, timeout)
	res := ProbeResult{Output: out, ExitCode: code, DurationMs: float64(time.Since(start).Milliseconds())}
	if err != nil {
		var exit *ssh.ExitError
		if errors.As(err, &exit) {
			return res, nil
		}
		return res, err
	}
	return res, nil
}

func (r *Router) Lab(force bool) (*LabData, error) {
	r.mu.Lock()
	meta := r.meta
	metaAt := r.metaAt
	r.mu.Unlock()
	if force || meta == nil || time.Since(metaAt) > 15*time.Second {
		if m, err := r.fetchMeta(); err == nil {
			meta = m
		} else if meta == nil {
			return nil, err
		}
	}
	if meta == nil {
		return nil, errNotConnected
	}
	snap := r.Snapshot()
	info := r.currentInfo()
	if info == nil {
		return nil, errNotConnected
	}
	return buildLab(meta, &snap, info), nil
}
