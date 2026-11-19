package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type savedConfig struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	User        string `json:"user"`
	Auth        string `json:"auth"`
	KeyPath     string `json:"keyPath"`
	Pinned      bool   `json:"pinned"`
	Fingerprint string `json:"fingerprint"`
	Saved       bool   `json:"saved"`
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
