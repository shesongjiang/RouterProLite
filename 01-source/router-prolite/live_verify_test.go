package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func liveDial(t *testing.T) (*Router, *ConnectionInfo) {
	t.Helper()
	in := ConnectionInput{Host: "192.168.100.1", Port: 22, User: "root", Auth: "none"}
	normalized, err := normalizeConnection(in)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	client, fp, err := dialSSH(normalized, "")
	if err != nil {
		alt := normalized
		alt.Auth = "agent"
		client, fp, err = dialSSH(alt, "")
	}
	if err != nil {
		home, _ := os.UserHomeDir()
		for _, name := range []string{"id_ed25519", "id_rsa", "id_ecdsa"} {
			key := filepath.Join(home, ".ssh", name)
			if _, statErr := os.Stat(key); statErr != nil {
				continue
			}
			alt := normalized
			alt.Auth = "key"
			alt.KeyPath = key
			client, fp, err = dialSSH(alt, "")
			if err == nil {
				normalized = alt
				break
			}
		}
	}
	if err != nil {
		t.Fatalf("ssh: %v", err)
	}
	r := newRouter()
	r.mu.Lock()
	r.client = client
	r.input = &normalized
	r.pinned = fp
	r.mu.Unlock()
	t.Cleanup(func() { _ = client.Close() })
	meta, err := r.fetchMeta()
	if err != nil {
		t.Fatalf("meta: %v", err)
	}
	info := meta.connectionInfo(normalized, fp)
	snap, err := r.collectSnapshot(meta, info)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	r.mu.Lock()
	r.info = info
	r.snap = snap
	r.mu.Unlock()
	return r, info
}

func TestLivePkgEnvFeeds(t *testing.T) {
	if os.Getenv("PROLITE_LIVE") != "1" {
		t.Skip("set PROLITE_LIVE=1 to run against the real router")
	}
	r, _ := liveDial(t)
	env, err := r.ReadPkgEnv()
	if err != nil {
		t.Fatalf("read pkg env: %v", err)
	}
	t.Logf("manager=%s arch=%s kernel=%s overlay=%dKB tmp=%dKB installed=%d", env.Manager, env.Arch, env.Kernel, env.OverlayKB, env.TmpKB, len(env.Installed))
	for _, f := range env.Feeds {
		t.Logf("feed %-12s %s", f.Name, f.URL)
	}
	if env.Manager == "" || env.Arch == "" {
		t.Fatalf("missing manager/arch: %+v", env)
	}
	if len(env.Feeds) == 0 {
		t.Fatalf("no feeds parsed (repositories.d missing?); notes=%v", env.Notes)
	}
}

func TestLiveConsole(t *testing.T) {
	if os.Getenv("PROLITE_LIVE") != "1" {
		t.Skip("set PROLITE_LIVE=1 to run against the real router")
	}
	r, info := liveDial(t)

	meta := r.currentMeta()
	if meta == nil {
		t.Fatal("meta nil")
	}
	t.Logf("board=%s model=%s kernel=%s fp=%s", meta.Board.BoardName, meta.Board.Model, meta.Board.Kernel, info.Fingerprint[:16])
	if meta.Board.Model == "" || meta.Board.Kernel == "" {
		t.Error("board model/kernel empty")
	}
	if len(meta.Init) == 0 || len(meta.UCI["network"]) == 0 {
		t.Error("meta init/uci empty")
	}
	running := 0
	for _, v := range meta.Running {
		if v {
			running++
		}
	}
	t.Logf("meta: init=%d running=%d caps(wifi=%v cell=%v sqm=%v adg=%v mwan3=%v proxy=%v)",
		len(meta.Init), running, meta.Caps.Wireless, meta.Caps.Cellular, meta.Caps.SQM,
		meta.Caps.AdGuard, meta.Caps.Mwan3, meta.Caps.Proxy)

	start := time.Now()
	c, err := r.Console(true)
	if err != nil {
		t.Fatalf("console: %v", err)
	}
	t.Logf("console fetched in %s", time.Since(start).Round(time.Millisecond))
	t.Logf("host=%s arch=%s cpu=%q cores=%d freq=%dMHz uptime=%ds",
		c.Hostname, c.Arch, c.CPUModel, c.CPUCores, c.CPUFreqMHz, c.Uptime)
	t.Logf("mem total=%dMB avail=%dMB swap=%dMB storage=%d mounts=%d",
		c.Memory.Total/1048576, c.Memory.Available/1048576, c.Memory.SwapTotal/1048576, len(c.Storage), len(c.Mounts))
	t.Logf("ifaces=%d packages=%d configs=%d processes=%d plugins=%d",
		len(c.Interfaces), c.PackageCount, len(c.Configs), len(c.Processes), len(c.Plugins))
	for _, it := range c.Interfaces {
		t.Logf("iface %-8s up=%-5v proto=%-6s dev=%-8s ips=%d gw=%v dns=%d cell=%v", it.Name, it.Up, it.Proto, it.Device, len(it.IPs), it.Gateway != "", len(it.DNS), it.CellModem)
	}
	t.Logf("modem present=%v installed=%v connected=%v model=%q sim=%q iface=%s mode=%q operator=%q rsrp=%d",
		meta.Modem.Present, meta.Modem.Installed, meta.Modem.Connected, meta.Modem.Model,
		meta.Modem.SIMStatus, meta.Modem.Interface, meta.Modem.Mode, meta.Modem.Operator, meta.Modem.RSRP)
	runPlugins := []string{}
	for _, p := range c.Plugins {
		if p.Running {
			runPlugins = append(runPlugins, p.Name+"@"+p.Version)
		}
	}
	t.Logf("plugins running: %s", strings.Join(runPlugins, ", "))
	t.Logf("wireless radios=%d ifaces=%d, dhcp pools=%d static=%d leases=%d, fw zones=%d rules=%d redirects=%d upnp=%v/%v/%v",
		len(c.Wireless.Radios), len(c.Wireless.Ifaces), len(c.DHCP.Pools), len(c.DHCP.StaticHosts),
		len(c.DHCP.Leases), len(c.Firewall.Zones), c.Firewall.RuleCount, c.Firewall.RedirectCount,
		c.Firewall.UPnPInstalled, c.Firewall.UPnPEnabled, c.Firewall.UPnPRunning)
	if c.PackageCount < 50 {
		t.Errorf("package count too low: %d", c.PackageCount)
	}
	if len(c.Interfaces) == 0 {
		t.Error("no interfaces parsed")
	}
	if c.Memory.Total == 0 {
		t.Error("memory total zero")
	}
	if c.Arch == "" {
		t.Error("arch empty")
	}
	if len(c.Plugins) == 0 {
		t.Error("no plugins detected")
	}

	configs, err := r.GetConfigs()
	if err != nil {
		t.Fatalf("configs: %v", err)
	}
	t.Logf("configs: %s", strings.Join(configs, " "))
	if len(configs) == 0 {
		t.Error("config list empty")
	}
	for _, name := range []string{"network", "firewall", "dhcp", "wireless", "system"} {
		vals, err := r.GetConfigValues(name)
		if err != nil {
			t.Errorf("config %s: %v", name, err)
			continue
		}
		t.Logf("config %s sections=%d", name, len(vals))
		if len(vals) == 0 && name != "wireless" {
			t.Errorf("config %s empty", name)
		}
	}

	logs, err := r.Logs("system", "", 40)
	if err != nil {
		t.Errorf("logs: %v", err)
	} else {
		t.Logf("system log bytes=%d lines=%d", len(logs), len(strings.Split(strings.TrimSpace(logs), "\n")))
	}
	klog, err := r.Logs("kernel", "", 20)
	if err != nil {
		t.Errorf("kernel log: %v", err)
	} else {
		t.Logf("kernel log bytes=%d", len(klog))
	}

	snap := r.Snapshot()
	sys := snap.System
	if sys == nil {
		sys = &SystemInfo{}
	}
	wan := snap.WAN
	if wan == nil {
		wan = &WANInfo{}
	}
	rates := snap.Rates
	if rates == nil {
		rates = &Rates{}
	}
	t.Logf("snapshot connected=%v tiles=%d clients=%d plugins=%d cpu=%.1f%% mem=%dMB/%dMB flash=%dKB/%dKB wanUp=%v proto=%s rateRx=%.0fB/s",
		snap.Connected, len(snap.Tiles), len(snap.Clients), len(snap.Plugins),
		sys.CPUPct, sys.MemUsed/1048576, sys.MemTotal/1048576,
		sys.FlashFree/1024, sys.FlashTotal/1024,
		wan.Up, wan.Proto, rates.WanRx)
	if sys.FlashTotal > 0 && sys.FlashFree > sys.FlashTotal {
		t.Errorf("flash free 大于 total：free=%d total=%d", sys.FlashFree, sys.FlashTotal)
	}
	for _, p := range snap.Ports {
		t.Logf("port %-5s up=%-5v state=%-6s speed=%-10s detail=%s", p.Key, p.Up, p.State, p.Speed, p.Detail)
	}
	if !snap.Connected {
		t.Error("snapshot not connected")
	}
	if snap.System == nil || snap.System.MemTotal == 0 {
		t.Error("snapshot system empty")
	}
	if len(snap.Tiles) == 0 {
		t.Error("snapshot tiles empty")
	}
	if len(snap.Ports) < 3 {
		t.Errorf("expected at least 3 ports, got %d", len(snap.Ports))
	}
	if len(snap.Ports) > 0 && (snap.Ports[0].Key != "wan" || snap.Ports[2].Key != "lan") {
		t.Errorf("unexpected port order: %+v", snap.Ports)
	}
}
