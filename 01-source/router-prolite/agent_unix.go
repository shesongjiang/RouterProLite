//go:build !windows

package main

import (
	"errors"
	"net"
	"os"
	"time"
)

func dialAgent() (net.Conn, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, errors.New("未设置 SSH_AUTH_SOCK，请启动 ssh-agent 或改用私钥文件")
	}
	return net.DialTimeout("unix", sock, 3*time.Second)
}
