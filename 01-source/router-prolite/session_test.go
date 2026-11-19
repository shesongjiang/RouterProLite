package main

import (
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestEnsureClientReusesPasswordCredentials(t *testing.T) {
	r := newRouter()
	r.input = &ConnectionInput{Host: "192.168.100.1", Port: 22, User: "root", Auth: "password", Password: "secret"}
	var got []ConnectionInput
	r.dial = func(in ConnectionInput, pinned string) (*ssh.Client, string, error) {
		got = append(got, in)
		return nil, "", errors.New("stub dial failure")
	}

	for i := 0; i < 2; i++ {
		if _, err := r.ensureClient(); err == nil {
			t.Fatalf("expected stub dial error")
		}
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 dial attempts, got %d", len(got))
	}
	for _, in := range got {
		if in.Auth != "password" || in.Password != "secret" {
			t.Fatalf("stored credentials not reused: %+v", in)
		}
	}
}

func TestEnsureClientAlternatesPasswordAndNone(t *testing.T) {
	r := newRouter()
	var got []ConnectionInput
	r.dial = func(in ConnectionInput, pinned string) (*ssh.Client, string, error) {
		got = append(got, in)
		return nil, "", errors.New("stub dial failure")
	}

	r.input = &ConnectionInput{Host: "10.0.0.1", Port: 22, User: "root", Auth: "password", Password: "pw-a"}
	_, _ = r.ensureClient()
	r.Disconnect()

	r.input = &ConnectionInput{Host: "10.0.0.2", Port: 22, User: "root", Auth: "none"}
	_, _ = r.ensureClient()

	if len(got) != 2 {
		t.Fatalf("expected 2 dials, got %d", len(got))
	}
	if got[0].Password != "pw-a" || got[0].Auth != "password" {
		t.Fatalf("password device credentials lost: %+v", got[0])
	}
	if got[1].Auth != "none" || got[1].Password != "" {
		t.Fatalf("passwordless device should stay none: %+v", got[1])
	}
}

func TestDisconnectClearsSessionCredentials(t *testing.T) {
	r := newRouter()
	r.input = &ConnectionInput{Host: "10.0.0.1", Port: 22, User: "root", Auth: "password", Password: "pw"}
	r.Disconnect()

	if r.input != nil || r.credExpired {
		t.Fatalf("credentials must be cleared on disconnect: %+v", r.input)
	}

	dialed := false
	r.dial = func(in ConnectionInput, pinned string) (*ssh.Client, string, error) {
		dialed = true
		return nil, "", errors.New("should not dial")
	}
	if _, err := r.ensureClient(); !errors.Is(err, errNotConnected) {
		t.Fatalf("expected errNotConnected, got %v", err)
	}
	if dialed {
		t.Fatalf("must not dial after disconnect")
	}
	if err := r.sessionAvailable(); !errors.Is(err, errNotConnected) {
		t.Fatalf("session should be unavailable: %v", err)
	}
}

func TestExpiredPasswordRequiresReentry(t *testing.T) {
	r := newRouter()
	r.input = &ConnectionInput{Host: "10.0.0.1", Port: 22, User: "root", Auth: "password", Password: "old"}
	r.dial = func(in ConnectionInput, pinned string) (*ssh.Client, string, error) {
		return nil, "", &authExpiredError{}
	}

	_, err := r.ensureClient()
	if err == nil || !strings.Contains(err.Error(), "需要重新输入密码") {
		t.Fatalf("expected re-entry message, got %v", err)
	}
	if r.input != nil || !r.credExpired {
		t.Fatalf("expired credentials should be dropped until user re-enters")
	}

	dialed := false
	r.dial = func(in ConnectionInput, pinned string) (*ssh.Client, string, error) {
		dialed = true
		return nil, "", errors.New("should not dial")
	}
	if _, err := r.ensureClient(); err == nil || !strings.Contains(err.Error(), "需要重新输入密码") {
		t.Fatalf("subsequent operations should keep the re-entry message, got %v", err)
	}
	if dialed {
		t.Fatalf("must not keep retrying an expired password")
	}
}

func TestBuildAuthNoneNeedsNoPassword(t *testing.T) {
	auths, closeAuth, err := buildAuth(ConnectionInput{Auth: "none"})
	defer closeAuth()
	if err != nil {
		t.Fatalf("none auth should not fail: %v", err)
	}
	if len(auths) != 0 {
		t.Fatalf("none auth should not register methods")
	}
}

func startTestSSH(t *testing.T, password string) (string, int, func()) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	cfg := &ssh.ServerConfig{}
	if password == "" {
		cfg.NoClientAuth = true
	} else {
		cfg.PasswordCallback = func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if string(pass) == password {
				return nil, nil
			}
			return nil, fmt.Errorf("password rejected")
		}
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				sconn, chans, reqs, err := ssh.NewServerConn(conn, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for ch := range chans {
					_ = ch.Reject(ssh.UnknownChannelType, "not implemented")
				}
				_ = sconn.Close()
			}()
		}
	}()

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	return host, port, func() { _ = ln.Close() }
}

func TestDialSSHPasswordAndNone(t *testing.T) {
	host, port, stop := startTestSSH(t, "pw")
	defer stop()
	client, fp, err := dialSSH(ConnectionInput{Host: host, Port: port, User: "root", Auth: "password", Password: "pw"}, "")
	if err != nil || client == nil || fp == "" {
		t.Fatalf("password dial failed: %v", err)
	}
	_ = client.Close()

	host, port, stop2 := startTestSSH(t, "")
	defer stop2()
	client, _, err = dialSSH(ConnectionInput{Host: host, Port: port, User: "root", Auth: "none"}, "")
	if err != nil || client == nil {
		t.Fatalf("passwordless dial failed: %v", err)
	}
	_ = client.Close()
}

func TestDialSSHExpiredPassword(t *testing.T) {
	host, port, stop := startTestSSH(t, "pw")
	defer stop()
	_, _, err := dialSSH(ConnectionInput{Host: host, Port: port, User: "root", Auth: "password", Password: "wrong"}, "")
	var ae *authExpiredError
	if !errors.As(err, &ae) {
		t.Fatalf("expected authExpiredError, got %v", err)
	}
	if !strings.Contains(err.Error(), "需要重新输入密码") {
		t.Fatalf("unexpected message: %v", err)
	}
}
