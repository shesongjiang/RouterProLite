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

func TestParseAddrIPv4(t *testing.T) {
	raw := "7: usb0    inet 192.168.4.170/24 brd 192.168.4.255 scope global usb0\\       valid_lft forever\n" +
		"8: br-lan    inet 192.168.100.1/24 brd 192.168.100.255 scope global br-lan\\       valid_lft forever\n"
	got := parseAddrIPv4(raw)
	if len(got["usb0"]) != 1 || got["usb0"][0] != "192.168.4.170/24" {
		t.Fatalf("usb0 addr: %v", got["usb0"])
	}
	if len(got["br-lan"]) != 1 || got["br-lan"][0] != "192.168.100.1/24" {
		t.Fatalf("br-lan addr: %v", got["br-lan"])
	}
}

func TestAttachModemInterfaceUsesMatchedLink(t *testing.T) {
	c := &ConsoleData{
		Interfaces: []InterfaceInfo{{
			Name: "wan", Device: "usb0", Up: true, Available: true, Proto: "dhcp",
			IPs: []string{"192.168.4.170/24"}, Gateway: "192.168.4.1",
			DNS: []string{"192.168.4.1"}, Uptime: 3600,
		}},
		Modem: CellularInfo{Installed: true, Present: true, Connected: true, Mode: "4G", Interface: "usb0"},
	}
	attachModemInterface(c, "7: usb0 inet 192.168.4.170/24", map[string]netCounter{"usb0": {rx: 123, tx: 456}})
	if len(c.Interfaces) != 2 || !c.Interfaces[0].CellModem {
		t.Fatalf("expected modem card first: %+v", c.Interfaces)
	}
	m := c.Interfaces[0]
	if m.Device != "usb0" || m.Proto != "4G" || !m.Up || len(m.IPs) != 1 || m.IPs[0] != "192.168.4.170/24" {
		t.Fatalf("unexpected modem card: %+v", m)
	}
	if m.RxBytes != 123 || m.TxBytes != 456 || m.Uptime != 3600 {
		t.Fatalf("modem counters/uptime: %+v", m)
	}
	if c.Modem.IPv4 != "192.168.4.170/24" || c.Modem.Gateway != "192.168.4.1" || c.Modem.Uptime != 3600 {
		t.Fatalf("modem link details: %+v", c.Modem)
	}
}

func TestAttachModemInterfaceAddrFallback(t *testing.T) {
	c := &ConsoleData{
		Modem: CellularInfo{Installed: true, Present: true, Mode: "5G", Interface: "wwan0"},
	}
	attachModemInterface(c, "9: wwan0 inet 10.20.30.40/30", nil)
	if len(c.Interfaces) != 1 || c.Interfaces[0].Name != "蜂窝网络" || c.Interfaces[0].Device != "wwan0" {
		t.Fatalf("expected synthetic modem card: %+v", c.Interfaces)
	}
	if c.Modem.IPv4 != "10.20.30.40/30" || c.Interfaces[0].IPs[0] != "10.20.30.40/30" {
		t.Fatalf("addr fallback failed: %+v", c.Modem)
	}
}

func TestAttachModemInterfaceSkipsWithoutStack(t *testing.T) {
	c := &ConsoleData{Interfaces: []InterfaceInfo{{Name: "lan", Device: "br-lan"}}}
	attachModemInterface(c, "", nil)
	if len(c.Interfaces) != 1 {
		t.Fatalf("must not add modem card without a modem stack: %+v", c.Interfaces)
	}
}
