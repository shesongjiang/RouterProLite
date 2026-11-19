//go:build windows

package main

import (
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

func dialAgent() (net.Conn, error) {
	timeout := 3 * time.Second
	return winio.DialPipe(`\\.\pipe\openssh-ssh-agent`, &timeout)
}
