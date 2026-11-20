package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

type savedConfig struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	User    string `json:"user"`
	Auth    string `json:"auth"`
	KeyPath string `json:"keyPath"`
	Pinned  bool   `json:"pinned"`
	// Fingerprint 是旧版的单设备全局指纹，仅作为迁移读取的回退，不再写入。
	Fingerprint string `json:"fingerprint,omitempty"`
	// Fingerprints 按 "host:port" 分别保存每台设备的 TOFU 指纹，多台设备互不干扰。
	Fingerprints map[string]string `json:"fingerprints,omitempty"`
	Saved        bool              `json:"saved"`
}

func defaultConfig() savedConfig {
	return savedConfig{
		Host: "192.168.100.1",
		Port: 22,
		User: "root",
		Auth: "agent",
	}
}

type configStore struct {
	mu   sync.Mutex
	path string
}

func newConfigStore() *configStore {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	base := filepath.Join(dir, "ProLite")
	_ = os.MkdirAll(base, 0o700)
	return &configStore{path: filepath.Join(base, "config.json")}
}

func (c *configStore) Load() savedConfig {
	c.mu.Lock()
	defer c.mu.Unlock()
	cfg := defaultConfig()
	data, err := os.ReadFile(c.path)
	if err != nil {
		return cfg
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return defaultConfig()
	}
	if cfg.Fingerprints == nil {
		cfg.Fingerprints = map[string]string{}
	}
	if cfg.Host == "" {
		cfg.Host = "192.168.100.1"
	}
	if cfg.Port <= 0 {
		cfg.Port = 22
	}
	if cfg.User == "" {
		cfg.User = "root"
	}
	if cfg.Auth == "" {
		cfg.Auth = "agent"
	}
	return cfg
}

func (c *configStore) Save(cfg savedConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	_ = os.Remove(c.path)
	return os.Rename(tmp, c.path)
}

func (c *configStore) Dir() string {
	return filepath.Dir(c.path)
}

func fpKey(host string, port int) string {
	return host + ":" + strconv.Itoa(port)
}

// fingerprintFor 返回指定设备的固定指纹；旧版单值指纹只对配置里保存的主机生效，
// 避免把 A 设备的指纹误用到 B 设备上。
func (c *savedConfig) fingerprintFor(host string, port int) string {
	if c.Fingerprints == nil {
		c.Fingerprints = map[string]string{}
	}
	if fp, ok := c.Fingerprints[fpKey(host, port)]; ok && fp != "" {
		return fp
	}
	if c.Fingerprint != "" && host == c.Host && port == c.Port {
		return c.Fingerprint
	}
	return ""
}

func (c *savedConfig) setFingerprint(host string, port int, fp string) {
	if c.Fingerprints == nil {
		c.Fingerprints = map[string]string{}
	}
	if fp == "" {
		delete(c.Fingerprints, fpKey(host, port))
		return
	}
	c.Fingerprints[fpKey(host, port)] = fp
}
