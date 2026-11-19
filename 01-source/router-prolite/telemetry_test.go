package main

import (
	"strings"
	"testing"
)

func TestParseCPUStat(t *testing.T) {
	stat, ok := parseCPUStat("cpu  100 0 50 850 20 0 0 0 0 0")
	if !ok {
		t.Fatal("expected stat")
	}
	if stat.total != 1020 || stat.idle != 870 {
		t.Fatalf("unexpected stat: %+v", stat)
	}
	if _, ok := parseCPUStat("cpu"); ok {
		t.Fatal("short line should fail")
	}
	if _, ok := parseCPUStat("intr 1 2 3"); ok {
		t.Fatal("non-cpu line should fail")
	}
}

func TestParseNetDev(t *testing.T) {
	raw := `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 1000 1 0 0 0 0 0 0 1000 1 0 0 0 0 0 0
  eth0: 2048 2 0 0 0 0 0 0 4096 3 0 0 0 0 0 0
`
	got := parseNetDev(raw)
	if got["lo"] != (netCounter{rx: 1000, tx: 1000}) {
		t.Fatalf("lo: %+v", got["lo"])
	}
	if got["eth0"] != (netCounter{rx: 2048, tx: 4096}) {
		t.Fatalf("eth0: %+v", got["eth0"])
	}
	if _, ok := got["Inter-"]; ok {
		t.Fatal("header parsed as interface")
	}
}

func TestParseLeasesAndARP(t *testing.T) {
	leases := parseLeases("1700000000 aa:bb:cc:dd:ee:ff 192.168.1.10 phone 01:aa:bb:cc:dd:ee:ff\n0 invalid 192.168.1.11 * *\n")
	if len(leases) != 1 {
		t.Fatalf("leases: %+v", leases)
	}
	if leases[0].mac != "aa:bb:cc:dd:ee:ff" || leases[0].name != "phone" || leases[0].ip != "192.168.1.10" {
		t.Fatalf("lease: %+v", leases[0])
	}

	arpRaw := `IP address       HW type     Flags       HW address            Mask     Device
192.168.1.10     0x1         0x2         aa:bb:cc:dd:ee:ff     *        br-lan
192.168.1.11     0x1         0x0         00:00:00:00:00:00     *        br-lan
`
	arp := parseARP(arpRaw)
	if len(arp) != 1 {
		t.Fatalf("arp: %+v", arp)
	}
	if a := arp["192.168.1.10"]; a.mac != "aa:bb:cc:dd:ee:ff" || !a.online || a.device != "br-lan" {
		t.Fatalf("arp entry: %+v", a)
	}
}

func TestParseMemInfo(t *testing.T) {
	total, avail := parseMemInfo("MemTotal:       1024000 kB\nMemFree:          100000 kB\nMemAvailable:     512000 kB\n")
	if total != 1024000*1024 || avail != 512000*1024 {
		t.Fatalf("mem: %d %d", total, avail)
	}
}

func TestParseSpeeds(t *testing.T) {
	got := parseSpeeds("wan 1000\nlan 100\nbad x\n")
	if got["wan"] != "1000M FULL" || got["lan"] != "100M" {
		t.Fatalf("speeds: %+v", got)
	}
	if _, ok := got["bad"]; ok {
		t.Fatal("invalid speed parsed")
	}
}

func TestParsePingMS(t *testing.T) {
	if v, ok := parsePingMS("64 bytes from 1.1.1.1: seq=0 ttl=57 time=12.3 ms"); !ok || v != 12.3 {
		t.Fatalf("ping: %v %v", v, ok)
	}
	if _, ok := parsePingMS("Request timeout"); ok {
		t.Fatal("timeout should not parse")
	}
}

func TestLatencyTargets(t *testing.T) {
	got := latencyTargets("192.168.100.1")
	if len(got) != 3 || got[2] != "192.168.100.1" {
		t.Fatalf("gateway target: %+v", got)
	}
	if got := latencyTargets("not-an-ip"); len(got) != 2 {
		t.Fatalf("invalid gateway should be ignored: %+v", got)
	}
	if got := latencyTargets("223.5.5.5"); len(got) != 2 {
		t.Fatalf("duplicate target should be skipped: %+v", got)
	}
	if got := latencyTargets("fe80::1"); len(got) != 2 {
		t.Fatalf("IPv6 gateway cannot be used as ping target: %+v", got)
	}
}

func TestLatencyCommand(t *testing.T) {
	cmd := latencyCommand(latencyTargets("192.168.1.1"))
	for _, want := range []string{"223.5.5.5", "1.1.1.1", "192.168.1.1", "break"} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("command %q missing %q", cmd, want)
		}
	}
}

func TestResolveMemory(t *testing.T) {
	mem := "MemTotal:       1024000 kB\nMemFree:          100000 kB\nMemAvailable:     512000 kB\n"
	total, avail := resolveMemory(0, 0, 0, mem)
	if total != 1024000*1024 || avail != 512000*1024 {
		t.Fatalf("fallback to meminfo: %d %d", total, avail)
	}
	total, avail = resolveMemory(2048000, 0, 300000, mem)
	if total != 2048000 || avail != 300000 {
		t.Fatalf("fallback to free: %d %d", total, avail)
	}
	_, avail = resolveMemory(2048000, 0, 0, mem)
	if avail != 512000*1024 {
		t.Fatalf("fallback to MemAvailable: %d", avail)
	}
}

func TestSplitSections(t *testing.T) {
	out := "noise\n@@A\nalpha\n@@B\nbeta\nline2\n@@END\n"
	got := splitSections(out)
	if got["A"] != "alpha" {
		t.Fatalf("A=%q", got["A"])
	}
	if got["B"] != "beta\nline2" {
		t.Fatalf("B=%q", got["B"])
	}
	if got["END"] != "" {
		t.Fatalf("END=%q", got["END"])
	}
}

func TestParseUCIAndFirewall(t *testing.T) {
	raw := `{"values":{
		"cfg1":{".type":"redirect","name":"web","src_dport":"8080","dest_ip":"192.168.1.2","dest_port":"80","proto":"tcp","enabled":"1"},
		"cfg2":{".type":"rule","name":"prolite-block-AA:BB:CC:DD:EE:FF","src_mac":"AA:BB:CC:DD:EE:FF","target":"DROP"},
		"cfg3":{".type":"defaults","fullcone":"1"}
	}}`
	uci := parseUCI(raw)
	if len(uci) != 3 {
		t.Fatalf("uci: %+v", uci)
	}
	m := &Meta{UCI: map[string]UCIConfig{"firewall": uci}, Init: map[string]bool{}, Enabled: map[string]bool{}, Running: map[string]bool{}}
	m.parseFirewall()
	if !m.Caps.FullCone || !m.FullCone {
		t.Fatal("fullcone not detected")
	}
	if len(m.Redirects) != 1 || m.Redirects[0].ExtPort != "8080" || m.Redirects[0].DestIP != "192.168.1.2" {
		t.Fatalf("redirects: %+v", m.Redirects)
	}
	if len(m.Blocked) != 1 || m.Blocked[0].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("blocked: %+v", m.Blocked)
	}
}

func TestParseDHCP(t *testing.T) {
	uci := parseUCI(`{"values":{
		"dns":{".type":"dnsmasq","server":["1.1.1.1","8.8.8.8"]},
		"host1":{".type":"host","name":"nas","ip":"192.168.1.5","mac":"AA-BB-CC-DD-EE-01"}
	}}`)
	m := &Meta{UCI: map[string]UCIConfig{"dhcp": uci}, Init: map[string]bool{}, Enabled: map[string]bool{}, Running: map[string]bool{}}
	m.parseDHCP()
	if len(m.DNS) != 2 || m.DNS[0] != "1.1.1.1" {
		t.Fatalf("dns: %+v", m.DNS)
	}
	if len(m.Hosts) != 1 || m.Hosts[0].MAC != "aa:bb:cc:dd:ee:01" || m.Hosts[0].Name != "nas" {
		t.Fatalf("hosts: %+v", m.Hosts)
	}
}

func TestNormalizeMAC(t *testing.T) {
	if got := normalizeMAC("AA-BB-CC-DD-EE-FF"); got != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("mac: %q", got)
	}
	if got := normalizeMAC("bad"); got != "" {
		t.Fatalf("invalid mac: %q", got)
	}
}

func TestExtractChipset(t *testing.T) {
	if got := extractChipset("MediaTek MT7621 ver:1 eco:3"); got != "MT7621" {
		t.Fatalf("chipset: %q", got)
	}
	if got := extractChipset("generic"); got != "" {
		t.Fatalf("chipset fallback: %q", got)
	}
}

func TestParseLinksAndAddrDevices(t *testing.T) {
	links := parseLinks("br-lan up\nwan down\n")
	if links["br-lan"] != "up" || links["wan"] != "down" {
		t.Fatalf("links: %+v", links)
	}
	raw := `2: br-lan    inet 192.168.100.1/24 brd 192.168.100.255 scope global br-lan
3: wan    inet 203.0.113.7/24 brd 203.0.113.255 scope global wan
`
	addrs := parseAddrDevices(raw)
	if !addrs["br-lan"] || !addrs["wan"] || addrs["lo"] {
		t.Fatalf("addrs: %+v", addrs)
	}
}

func TestBuildPorts(t *testing.T) {
	meta := &Meta{
		UCI:      map[string]UCIConfig{"network": {"wan6": {".type": "interface"}}},
		Wireless: WirelessInfo{Present: true, Enabled: true, Channel: "149", Radios: []string{"radio0", "radio1"}},
		Caps:     Capabilities{Wireless: true, Cellular: true},
		Modem:    CellularInfo{Present: true, Internet: true, Count: 1},
	}
	wan := &WANInfo{Up: true, Status: "UP", Proto: "dhcp", Device: "wan", LinkSpeed: "1000M FULL"}
	rates := &Rates{WanRx: 100, WanTx: 50, LanRx: 20, LanTx: 10}
	clients := []Client{{Online: true}, {Online: true}, {Online: false}}
	ports := buildPorts(meta, wan, &WANInfo{Up: true, Status: "UP", Proto: "dhcpv6"}, rates, clients, "br-lan", true, map[string]string{"br-lan": "1000M FULL"})
	if len(ports) != 5 {
		t.Fatalf("expected 5 ports, got %d: %+v", len(ports), ports)
	}
	if ports[0].Key != "wan" || !ports[0].Up || ports[0].Speed != "1000M FULL" || ports[0].Detail != "dhcp · wan" {
		t.Fatalf("wan port: %+v", ports[0])
	}
	if ports[1].Key != "wan6" || !ports[1].Up || ports[1].Detail != "IPv6 · dhcpv6" {
		t.Fatalf("wan6 port: %+v", ports[1])
	}
	if ports[2].Key != "lan" || ports[2].Clients != 2 || ports[2].Detail != "br-lan · 2 台在线" {
		t.Fatalf("lan port: %+v", ports[2])
	}
	if ports[3].Key != "wifi" || ports[3].State != "UP" || ports[3].Detail != "CH 149 · 2 射频" {
		t.Fatalf("wifi port: %+v", ports[3])
	}
	if ports[4].Key != "cell" || !ports[4].Up {
		t.Fatalf("cellular port: %+v", ports[4])
	}

	empty := &Meta{UCI: map[string]UCIConfig{"network": {}}}
	ports = buildPorts(empty, &WANInfo{Up: false, Status: "DOWN"}, &WANInfo{}, &Rates{}, nil, "br-lan", false, nil)
	if len(ports) != 3 {
		t.Fatalf("expected 3 ports without wireless/cellular, got %d", len(ports))
	}
	if ports[1].State != "未配置" {
		t.Fatalf("unexpected states: %+v", ports)
	}
	for _, p := range ports {
		if p.Key == "wifi" || p.Key == "cell" {
			t.Fatalf("unexpected radio port without capabilities: %+v", p)
		}
	}
}
