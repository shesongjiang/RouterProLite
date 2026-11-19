package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

const appVersion = "0.1.0"

var (
	hostRe = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,253}$`)
	userRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)
)

type fingerprintError struct {
	Saved   string
	Current string
}

func (e *fingerprintError) Error() string {
	return fmt.Sprintf("主机密钥不匹配：已保存 %s，当前设备 %s。如果设备刚重装系统，请重置主机指纹后重试。", e.Saved, e.Current)
}

// authExpiredError 表示会话里保存的口令已无法通过认证，需要用户重新输入。
type authExpiredError struct {
	cause error
}

func (e *authExpiredError) Error() string {
	return "登录凭据已失效，需要重新输入密码后重连"
}

func (e *authExpiredError) Unwrap() error { return e.cause }

func isAuthFailure(err error) bool {
	if err == nil {
		return false
	}
	var authErr *ssh.ServerAuthError
	if errors.As(err, &authErr) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unable to authenticate") ||
		strings.Contains(s, "no supported methods remain") ||
		strings.Contains(s, "handshake failed")
}

func normalizeConnection(in ConnectionInput) (ConnectionInput, error) {
	in.Host = strings.TrimSpace(in.Host)
	in.User = strings.TrimSpace(in.User)
	in.KeyPath = strings.TrimSpace(in.KeyPath)
	if !hostRe.MatchString(in.Host) || strings.HasPrefix(in.Host, "-") {
		return in, errors.New("主机地址格式不正确")
	}
	if in.Port == 0 {
		in.Port = 22
	}
	if in.Port < 1 || in.Port > 65535 {
		return in, errors.New("SSH 端口必须在 1-65535 之间")
	}
	if !userRe.MatchString(in.User) {
		return in, errors.New("用户名格式不正确")
	}
	switch in.Auth {
	case "none":
	case "agent":
	case "key":
		if in.KeyPath == "" {
			return in, errors.New("请选择 SSH 私钥文件")
		}
		path := expandPath(in.KeyPath)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return in, errors.New("SSH 私钥文件不存在")
		}
		in.KeyPath = path
	case "password":
		if in.Password == "" {
			return in, errors.New("请输入 SSH 密码")
		}
	default:
		return in, errors.New("不支持的认证方式")
	}
	return in, nil
}

func expandPath(p string) string {
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func buildAuth(in ConnectionInput) ([]ssh.AuthMethod, func(), error) {
	switch in.Auth {
	case "none":
		return nil, func() {}, nil
	case "agent":
		conn, err := dialAgent()
		if err != nil {
			return nil, func() {}, fmt.Errorf("无法连接系统 SSH Agent：%w", err)
		}
		ag := agent.NewClient(conn)
		signers, err := ag.Signers()
		if err != nil {
			_ = conn.Close()
			return nil, func() {}, fmt.Errorf("读取 SSH Agent 密钥失败：%w", err)
		}
		if len(signers) == 0 {
			_ = conn.Close()
			return nil, func() {}, errors.New("SSH Agent 中没有可用密钥，请先 ssh-add 或改用私钥文件")
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signers...)}, func() { _ = conn.Close() }, nil

	case "key":
		data, err := os.ReadFile(in.KeyPath)
		if err != nil {
			return nil, func() {}, fmt.Errorf("读取私钥失败：%w", err)
		}
		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			var missing *ssh.PassphraseMissingError
			if errors.As(err, &missing) {
				if in.Passphrase == "" {
					return nil, func() {}, errors.New("私钥已加密，请输入私钥口令")
				}
				signer, err = ssh.ParsePrivateKeyWithPassphrase(data, []byte(in.Passphrase))
			}
			if err != nil {
				return nil, func() {}, fmt.Errorf("解析私钥失败：%w", err)
			}
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, func() {}, nil

	case "password":
		return []ssh.AuthMethod{ssh.Password(in.Password)}, func() {}, nil
	}
	return nil, func() {}, errors.New("不支持的认证方式")
}

func dialSSH(in ConnectionInput, pinned string) (*ssh.Client, string, error) {
	auths, closeAuth, err := buildAuth(in)
	if err != nil {
		return nil, "", err
	}
	defer closeAuth()

	var captured string
	hostKeyCallback := func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		current := ssh.FingerprintSHA256(key)
		if pinned != "" && pinned != current {
			return &fingerprintError{Saved: pinned, Current: current}
		}
		captured = current
		return nil
	}

	cfg := &ssh.ClientConfig{
		User:            in.User,
		Auth:            auths,
		HostKeyCallback: hostKeyCallback,
		Timeout:         8 * time.Second,
	}
	addr := net.JoinHostPort(in.Host, strconv.Itoa(in.Port))
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		if in.Auth == "password" && isAuthFailure(err) {
			return nil, "", &authExpiredError{cause: err}
		}
		return nil, "", err
	}
	if captured == "" {
		captured = pinned
	}
	return client, captured, nil
}
