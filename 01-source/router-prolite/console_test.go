package main

import "testing"

func TestParsePackagesAPK(t *testing.T) {
	raw := `base-files-1556~r39235
busybox-1.37.0-r1
kmod-nf-nathelper-6.6.1-r1
libc-1.2.5-r3
zoneinfo-2024b-r0
`
	pkgs := parsePackages(raw)
	if len(pkgs) != 5 {
		t.Fatalf("expected 5 packages, got %d: %+v", len(pkgs), pkgs)
	}
	byName := map[string]string{}
	for _, p := range pkgs {
		byName[p.Name] = p.Version
	}
	want := map[string]string{
		"base-files":        "1556~r39235",
		"busybox":           "1.37.0-r1",
		"kmod-nf-nathelper": "6.6.1-r1",
		"libc":              "1.2.5-r3",
		"zoneinfo":          "2024b-r0",
	}
	for name, version := range want {
		if byName[name] != version {
			t.Errorf("%s: got %q want %q", name, byName[name], version)
		}
	}
	if pkgs[0].Name != "base-files" {
		t.Errorf("packages should be sorted, first = %q", pkgs[0].Name)
	}
}

func TestParsePackagesOpkg(t *testing.T) {
	raw := "busybox - 1.36.1-1\nluci-base - git-24.001.1\n"
	pkgs := parsePackages(raw)
	if len(pkgs) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(pkgs))
	}
	if pkgs[0].Name != "busybox" || pkgs[0].Version != "1.36.1-1" {
		t.Errorf("unexpected busybox entry: %+v", pkgs[0])
	}
}

func TestParseCPUInfoMIPS(t *testing.T) {
	raw := `system type		: MediaTek MT7621 ver:1 eco:3
machine			: MelonPi M21 16M
processor		: 0
processor		: 1
processor		: 2
processor		: 3
`
	cores, model := parseCPUInfo(raw)
	if cores != 4 {
		t.Errorf("cores = %d, want 4", cores)
	}
	if model != "MediaTek MT7621 ver:1 eco:3" {
		t.Errorf("model = %q", model)
	}
}

func TestParseCPUInfoX86(t *testing.T) {
	raw := `processor	: 0
model name	: Intel(R) N100
cpu cores	: 4
processor	: 1
model name	: Intel(R) N100
`
	cores, model := parseCPUInfo(raw)
	if cores != 2 {
		t.Errorf("cores = %d, want 2", cores)
	}
	if model != "Intel(R) N100" {
		t.Errorf("model = %q", model)
	}
}

func TestParseDHCPDetailPoolTypes(t *testing.T) {
	meta := &Meta{UCI: map[string]UCIConfig{
		"dhcp": {
			"lan":   {"interface": "lan", "start": "100", "limit": "150", "leasetime": "12h"},
			"wan":   {"interface": "wan", "ignore": "1"},
			"fixed": {"name": "nas", "ip": "192.168.1.10", "mac": "AA:BB:CC:DD:EE:FF"},
		},
	}}
	meta.UCI["dhcp"]["lan"][".type"] = "dhcp"
	meta.UCI["dhcp"]["lan"][".index"] = float64(0)
	meta.UCI["dhcp"]["wan"][".type"] = "dhcp"
	meta.UCI["dhcp"]["wan"][".index"] = float64(1)
	meta.UCI["dhcp"]["fixed"][".type"] = "host"
	meta.UCI["dhcp"]["fixed"][".index"] = float64(2)

	d := parseDHCPDetail(meta, nil)
	if len(d.Pools) != 2 {
		t.Fatalf("expected 2 pools, got %d: %+v", len(d.Pools), d.Pools)
	}
	if d.Pools[0].Start != "100" || d.Pools[0].Limit != "150" || d.Pools[0].LeaseTime != "12h" {
		t.Errorf("unexpected pool: %+v", d.Pools[0])
	}
	if len(d.StaticHosts) != 1 || d.StaticHosts[0].Name != "nas" {
		t.Errorf("unexpected static hosts: %+v", d.StaticHosts)
	}
	if !d.Pools[1].Ignore {
		t.Errorf("pool with ignore=1 should be marked ignored: %+v", d.Pools[1])
	}
}

func TestParseSystemSettings(t *testing.T) {
	raw := `{"values":{"cfg01":{"hostname":"MyRouter","timezone":"CST-8","zonename":"Asia/Shanghai",".type":"system"},"ntp":{"enabled":"1","server":["ntp.aliyun.com","time.ustc.edu.cn"],".type":"ntp"}}}`
	section, ntpSection, hostname, timezone, zonename, ntpEnabled, servers := parseSystemSettings(raw)
	if section != "cfg01" || hostname != "MyRouter" || timezone != "CST-8" || zonename != "Asia/Shanghai" {
		t.Fatalf("unexpected system settings: %q %q %q %q", section, hostname, timezone, zonename)
	}
	if ntpSection != "ntp" || !ntpEnabled || len(servers) != 2 || servers[0] != "ntp.aliyun.com" {
		t.Fatalf("unexpected ntp settings: %q enabled=%v servers=%v", ntpSection, ntpEnabled, servers)
	}
}
