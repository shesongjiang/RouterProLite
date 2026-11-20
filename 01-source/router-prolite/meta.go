package main

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type BoardInfo struct {
	Kernel     string            `json:"kernel"`
	Hostname   string            `json:"hostname"`
	System     string            `json:"system"`
	Model      string            `json:"model"`
	BoardName  string            `json:"board_name"`
	RootfsType string            `json:"rootfs_type"`
	Release    map[string]string `json:"release"`
}

type UCIConfig map[string]map[string]any

type staticHost struct {
	Section string
	Name    string
	IP      string
	MAC     string
}

type Meta struct {
	Board     BoardInfo
	Hostname  string
	Caps      Capabilities
	Init      map[string]bool
	Enabled   map[string]bool
	Running   map[string]bool
	UCI       map[string]UCIConfig
	Resolv    []string
	Temp      bool
	MwanText  string
	Mwan      MWANInfo
	Modem     CellularInfo
	Wireless  WirelessInfo
	FullCone  bool
	DNS       []string
	Redirects []PortForward
	Hosts     []staticHost
	Blocked   []BlockRule
	DDNS      DDNSInfo
	WANProto  string
	WANDevice string
	Chipset   string
	FetchedAt time.Time
}

const metaScript = `echo '@@HOST'; cat /proc/sys/kernel/hostname 2>/dev/null
echo '@@BOARD'; ubus call system board 2>/dev/null
echo '@@RELEASE'; cat /etc/openwrt_release 2>/dev/null
echo '@@INITS'; ls /etc/init.d 2>/dev/null
echo '@@ENABLED'; for f in /etc/rc.d/S*; do [ -e "$f" ] || continue; n=${f##*/}; n=${n#S}; n=${n#[0-9][0-9]}; echo "1 $n"; done
echo '@@SERVICES'; ubus call service list 2>/dev/null
echo '@@UCI_DHCP'; ubus call uci get '{"config":"dhcp"}' 2>/dev/null
echo '@@UCI_FW'; ubus call uci get '{"config":"firewall"}' 2>/dev/null
echo '@@UCI_NET'; ubus call uci get '{"config":"network"}' 2>/dev/null
echo '@@UCI_WL'; ubus call uci get '{"config":"wireless"}' 2>/dev/null
echo '@@UCI_DDNS'; ubus call uci get '{"config":"ddns"}' 2>/dev/null
echo '@@RADIOS'; ls /sys/class/ieee80211 2>/dev/null
echo '@@WIFIFACES'; ls /sys/class/net 2>/dev/null | grep -E '^(wlan|ra[0-9]|rai|phy[0-9]|ath|apcli|wl)' 2>/dev/null
echo '@@TEMP'; ls /sys/class/thermal/thermal_zone*/temp 2>/dev/null | head -1
echo '@@TC'; command -v tc 2>/dev/null
echo '@@RESOLV'; cat /tmp/resolv.conf.d/resolv.conf.auto 2>/dev/null
echo '@@MWAN'; mwan3 status 2>/dev/null
echo '@@MODEM'; ubus call qmodem info 2>/dev/null
echo '@@MODEMCTRL'; ubus call modem_ctrl info 2>/dev/null
echo '@@QMODEM_NETIF'; ubus call qmodem get_network_interfaces 2>/dev/null
echo '@@QMODEM_SECS'; for s in $(uci show qmodem 2>/dev/null | sed -n -e 's/^qmodem\.\([^.=]*\)=modem$/\1/p' -e 's/^qmodem\.\([^.=]*\)=modem-device$/\1/p'); do echo "##SEC $s"; ubus call qmodem base_info "{\"config_section\":\"$s\"}" 2>/dev/null; ubus call qmodem cell_info "{\"config_section\":\"$s\"}" 2>/dev/null; ubus call qmodem network_info "{\"config_section\":\"$s\"}" 2>/dev/null; ubus call qmodem dial_status "{\"config_section\":\"$s\"}" 2>/dev/null; ubus call qmodem get_connect_status "{\"config_section\":\"$s\"}" 2>/dev/null; done
echo '@@QMODEM_UCI'; uci -q show qmodem 2>/dev/null
echo '@@MODEMDEVS'; ls /sys/class/net 2>/dev/null | grep -E '^(wwan|rmnet)' 2>/dev/null; ls /dev/cdc-wdm* 2>/dev/null
echo '@@UBUSMODEM'; ubus list 2>/dev/null | grep -E '^(qmodem|modem_ctrl|cpe_network)$'
echo '@@CPE'; ubus call cpe_network status 2>/dev/null
echo '@@END'; true`

func splitSections(out string) map[string]string {
	sections := map[string]string{}
	var cur string
	var b strings.Builder
	flush := func() {
		if cur != "" {
			sections[cur] = strings.TrimSpace(b.String())
		}
		b.Reset()
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "@@") {
			flush()
			cur = strings.TrimSpace(strings.TrimPrefix(line, "@@"))
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	flush()
	return sections
}

func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "1"
		}
		return "0"
	case float64:
		if t == math.Trunc(t) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return t.String()
	case []any:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			if s := asString(e); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, " ")
	default:
		return fmt.Sprint(t)
	}
}

func asStrings(v any) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s := asString(e); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		s := strings.TrimSpace(asString(v))
		if s == "" {
			return nil
		}
		return strings.Fields(s)
	}
}

func uciType(sec map[string]any) string {
	return asString(sec[".type"])
}

func uciEnabled(sec map[string]any) bool {
	if v, ok := sec["enabled"]; ok {
		s := asString(v)
		return s != "0" && s != "false" && s != "no"
	}
	return true
}

func parseUCI(raw string) UCIConfig {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var resp struct {
		Values map[string]map[string]any `json:"values"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil
	}
	return resp.Values
}

func parseShellKV(raw string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		val = strings.Trim(val, `"'`)
		out[key] = val
	}
	return out
}

func parseMetaOutput(out string) *Meta {
	s := splitSections(out)
	m := &Meta{
		Init:      map[string]bool{},
		Enabled:   map[string]bool{},
		Running:   map[string]bool{},
		UCI:       map[string]UCIConfig{},
		Temp:      strings.TrimSpace(s["TEMP"]) != "",
		FetchedAt: time.Now(),
	}

	_ = json.Unmarshal([]byte(s["BOARD"]), &m.Board)
	m.Hostname = strings.TrimSpace(s["HOST"])
	if m.Hostname == "" {
		m.Hostname = m.Board.Hostname
	}
	release := parseShellKV(s["RELEASE"])
	if m.Board.Release == nil {
		m.Board.Release = map[string]string{}
	}
	if m.Board.Kernel == "" {
		m.Board.Kernel = "unknown"
	}
	if m.Board.Release["distribution"] == "" {
		m.Board.Release["distribution"] = release["DISTRIB_ID"]
	}
	if m.Board.Release["description"] == "" {
		m.Board.Release["description"] = release["DISTRIB_DESCRIPTION"]
	}
	m.Chipset = extractChipset(m.Board.System)
	if m.Chipset == "" {
		m.Chipset = strings.ToUpper(strings.TrimSpace(m.Board.BoardName))
	}

	for _, line := range strings.Split(s["INITS"], "\n") {
		name := strings.TrimSpace(line)
		if name != "" {
			m.Init[name] = true
		}
	}
	for _, line := range strings.Split(s["ENABLED"], "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			m.Enabled[fields[1]] = fields[0] == "1"
		}
	}

	var services map[string]struct {
		Instances map[string]struct {
			Running bool `json:"running"`
		} `json:"instances"`
	}
	if err := json.Unmarshal([]byte(s["SERVICES"]), &services); err == nil {
		for name, svc := range services {
			running := false
			for _, inst := range svc.Instances {
				if inst.Running {
					running = true
					break
				}
			}
			m.Running[name] = running
		}
	}

	m.UCI["dhcp"] = parseUCI(s["UCI_DHCP"])
	m.UCI["firewall"] = parseUCI(s["UCI_FW"])
	m.UCI["network"] = parseUCI(s["UCI_NET"])
	m.UCI["wireless"] = parseUCI(s["UCI_WL"])
	m.UCI["ddns"] = parseUCI(s["UCI_DDNS"])

	for _, line := range strings.Split(s["RESOLV"], "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "nameserver" {
			m.Resolv = append(m.Resolv, fields[1])
		}
	}

	m.Caps = Capabilities{
		DNSMasq:        m.Init["dnsmasq"],
		Mwan3:          m.Init["mwan3"],
		SQM:            m.Init["sqm"],
		AdGuard:        m.Init["AdGuardHome"] || m.Init["adguardhome"],
		DoH:            m.Init["https-dns-proxy"],
		DDNS:           m.Init["ddns"] || len(m.UCI["ddns"]) > 0,
		TrafficControl: strings.TrimSpace(s["TC"]) != "",
		Temperature:    m.Temp,
		Proxy:          m.Init["passwall"] || m.Init["shellcrash"] || m.Init["openclash"] || m.Init["clash"],
	}
	m.Caps.Wireless = len(strings.Fields(s["RADIOS"])) > 0 || len(strings.Fields(s["WIFIFACES"])) > 0 || hasWifiDevice(m.UCI["wireless"])

	m.parseFirewall()
	m.parseDHCP()
	m.parseWireless()
	m.parseDDNS()
	m.parseWAN()
	m.parseModem(s)
	m.parseMwan(s["MWAN"])

	return m
}

var chipsetRe = regexp.MustCompile(`(?i)\b([A-Z]{2,6}[0-9]{3,5}[A-Z0-9]*)\b`)

func extractChipset(system string) string {
	if mt := chipsetRe.FindStringSubmatch(system); len(mt) > 1 {
		return strings.ToUpper(mt[1])
	}
	return ""
}

func hasWifiDevice(cfg UCIConfig) bool {
	for _, sec := range cfg {
		if uciType(sec) == "wifi-device" {
			return true
		}
	}
	return false
}

func (m *Meta) parseFirewall() {
	fw := m.UCI["firewall"]
	for id, sec := range fw {
		switch uciType(sec) {
		case "defaults":
			if v, ok := sec["fullcone"]; ok {
				m.Caps.FullCone = true
				m.FullCone = asString(v) == "1"
			}
		case "redirect":
			pf := PortForward{
				ID:       id,
				Name:     asString(sec["name"]),
				Proto:    asString(sec["proto"]),
				ExtPort:  asString(sec["src_dport"]),
				DestIP:   asString(sec["dest_ip"]),
				DestPort: asString(sec["dest_port"]),
				Enabled:  uciEnabled(sec),
			}
			if pf.Proto == "" {
				pf.Proto = "tcp"
			}
			if pf.ExtPort == "" {
				pf.ExtPort = pf.DestPort
			}
			if pf.Name == "" {
				pf.Name = "转发 " + pf.ExtPort
			}
			m.Redirects = append(m.Redirects, pf)
		case "rule":
			name := asString(sec["name"])
			mac := normalizeMAC(asString(sec["src_mac"]))
			if mac != "" && strings.HasPrefix(name, "prolite-block-") && strings.EqualFold(asString(sec["target"]), "DROP") {
				m.Blocked = append(m.Blocked, BlockRule{MAC: mac, Section: id})
			}
		}
	}
	sort.Slice(m.Redirects, func(i, j int) bool {
		ai := indexOf(fw[m.Redirects[i].ID])
		aj := indexOf(fw[m.Redirects[j].ID])
		return ai < aj
	})
	sort.Slice(m.Blocked, func(i, j int) bool { return m.Blocked[i].MAC < m.Blocked[j].MAC })
}

func indexOf(sec map[string]any) int {
	if sec == nil {
		return 0
	}
	if f, ok := sec[".index"].(float64); ok {
		return int(f)
	}
	return 0
}

func (m *Meta) parseDHCP() {
	dhcp := m.UCI["dhcp"]
	for id, sec := range dhcp {
		switch uciType(sec) {
		case "dnsmasq":
			if servers := asStrings(sec["server"]); len(servers) > 0 {
				m.DNS = servers
			}
		case "host":
			h := staticHost{
				Section: id,
				Name:    asString(sec["name"]),
				IP:      asString(sec["ip"]),
				MAC:     normalizeMAC(asString(sec["mac"])),
			}
			if h.MAC != "" {
				m.Hosts = append(m.Hosts, h)
			}
		}
	}
}

func (m *Meta) parseWireless() {
	wl := m.UCI["wireless"]
	info := WirelessInfo{Present: m.Caps.Wireless}
	for id, sec := range wl {
		switch uciType(sec) {
		case "wifi-device":
			info.Radios = append(info.Radios, id)
			if ch := asString(sec["channel"]); ch != "" && ch != "auto" && info.Channel == "" {
				info.Channel = ch
			}
			if uciEnabled(sec) && asString(sec["disabled"]) != "1" {
				info.Enabled = true
			}
		case "wifi-iface":
			mode := asString(sec["mode"])
			if mode == "" || mode == "ap" {
				if ssid := asString(sec["ssid"]); ssid != "" && info.SSID == "" && asString(sec["disabled"]) != "1" {
					info.SSID = ssid
					info.Enabled = true
				}
			}
		}
	}
	m.Wireless = info
	if info.Enabled {
		m.Caps.Wireless = true
	}
}

func (m *Meta) parseDDNS() {
	cfg := m.UCI["ddns"]
	if !m.Caps.DDNS {
		m.DDNS = DDNSInfo{Installed: false}
		return
	}
	out := DDNSInfo{Installed: true}
	running := m.Running["ddns"]
	for id, sec := range cfg {
		if uciType(sec) != "service" {
			continue
		}
		out.Services = append(out.Services, DDNSService{
			Name:    id,
			Domain:  asString(sec["domain"]),
			Enabled: uciEnabled(sec),
			Running: running,
		})
	}
	m.DDNS = out
}

func (m *Meta) parseWAN() {
	netCfg := m.UCI["network"]
	for id, sec := range netCfg {
		if uciType(sec) != "interface" {
			continue
		}
		if id == "wan" || asString(sec["interface"]) == "wan" || id == "network.wan" {
			if v := asString(sec["proto"]); v != "" {
				m.WANProto = v
			}
			if v := asString(sec["device"]); v != "" {
				m.WANDevice = v
			}
		}
	}
	if m.WANDevice == "" {
		m.WANDevice = "wan"
	}
	if m.WANProto == "" {
		m.WANProto = "dhcp"
	}
}

func (m *Meta) parseModem(s map[string]string) {
	raw := s["MODEM"]
	ctrlRaw := s["MODEMCTRL"]
	secsRaw := s["QMODEM_SECS"]
	netifRaw := s["QMODEM_NETIF"]
	uciRaw := s["QMODEM_UCI"]
	devsRaw := s["MODEMDEVS"]
	ubusRaw := s["UBUSMODEM"]
	cpeRaw := s["CPE"]

	cell := CellularInfo{Installed: m.Init["qmodem"] || m.Init["modem_ctrl"]}
	acc := &modemSignals{}
	scan := &modemScan{cell: &cell, acc: acc}
	var raws []string

	// QModem Next 把模组配置写成 modem-device，旧版 QModem 用 modem。
	sections, secIface := parseQmodemSections(uciRaw)
	if len(sections) > 0 {
		cell.Installed = true
		cell.Devices = append(cell.Devices, sections...)
	}
	// 设备上真正暴露的 ubus 对象：modem_ctrl 属于 QModem Next，cpe_network 属于 CPE 面板。
	if hasUbusObject(ubusRaw, "qmodem") || hasUbusObject(ubusRaw, "modem_ctrl") || hasUbusObject(ubusRaw, "cpe_network") {
		cell.Installed = true
	}

	var q struct {
		ModemInfo []map[string]any `json:"modem_info"`
	}
	if err := json.Unmarshal([]byte(raw), &q); err == nil && len(q.ModemInfo) > 0 {
		cell.Count = len(q.ModemInfo)
		raws = append(raws, raw)
		for _, modem := range q.ModemInfo {
			for _, key := range []string{"config_section", "device", "tty", "usb_path", "imei"} {
				if v := asString(modem[key]); v != "" {
					cell.Devices = append(cell.Devices, v)
					break
				}
			}
			scanModemSignals(modem, acc)
		}
	}

	// QModem Next：modem_ctrl info 返回 {info:[{modem_info:[{key,value}]}]}。
	var mc struct {
		Info []struct {
			ModemInfo []map[string]any `json:"modem_info"`
		} `json:"info"`
	}
	if err := json.Unmarshal([]byte(ctrlRaw), &mc); err == nil && len(mc.Info) > 0 {
		cell.Installed = true
		if cell.Count == 0 {
			cell.Count = len(mc.Info)
		}
		raws = append(raws, ctrlRaw)
		for _, modem := range mc.Info {
			for _, entry := range modem.ModemInfo {
				if key := firstString(entry, "key"); key != "" {
					scan.field(key, entry["value"])
					continue
				}
				if v, ok := entry["value"]; ok {
					if label := firstString(entry, "name", "full_name"); label != "" {
						scan.field(label, v)
						continue
					}
				}
				scanModemSignals(entry, acc)
			}
		}
	}

	// 旧版 QModem 按配置段输出的 base_info / cell_info / network_info / dial_status。
	for _, chunk := range strings.Split(secsRaw, "##SEC ")[1:] {
		name := ""
		body := chunk
		if i := strings.IndexByte(chunk, '\n'); i >= 0 {
			name = strings.TrimSpace(chunk[:i])
			body = chunk[i+1:]
		}
		_ = name
		raws = append(raws, chunk)
		gotJSON := false
		for _, v := range decodeJSONStream(body) {
			gotJSON = true
			if mm, ok := v.(map[string]any); ok {
				if dev := asString(mm["device"]); strings.HasPrefix(dev, "/dev/") {
					cell.Devices = appendUnique(cell.Devices, dev)
				}
			}
			scanModemSignals(v, acc)
		}
		if gotJSON && cell.Count == 0 {
			cell.Count = 1
		}
	}

	// CPE 面板：cpe_network status 的 cellular 是这台设备实际使用的字段集合。
	var cpe struct {
		Cellular map[string]any `json:"cellular"`
	}
	cpePresent := false
	if err := json.Unmarshal([]byte(cpeRaw), &cpe); err == nil && len(cpe.Cellular) > 0 {
		c := cpe.Cellular
		raws = append(raws, cpeRaw)
		if b, ok := c["present"].(bool); ok && b {
			cpePresent = true
			if cell.Count == 0 {
				cell.Count = 1
			}
		}
		if v := asString(c["device"]); v != "" {
			if acc.iface == "" {
				acc.iface = v
			}
			cell.Devices = appendUnique(cell.Devices, v)
		}
		if v := asString(c["model"]); v != "" && cell.Model == "" {
			cell.Model = v
		}
		if v := asString(c["sim_status"]); v != "" {
			cell.SIMStatus = v
		}
		if v := asString(c["band"]); v != "" {
			cell.Band = v
		}
		if v := asString(c["radio"]); v != "" && acc.mode == "" {
			acc.mode = normalizeMode(v)
		}
		if valid, _ := c["signal_valid"].(bool); valid {
			if n, ok := intValue(c["nr_signal"]); ok && n != 0 {
				acc.setInt(&acc.rsrp, &acc.hasRSRP, n)
				if acc.mode == "" {
					acc.mode = "5G"
				}
			} else if n, ok := intValue(c["lte_signal"]); ok && n != 0 {
				acc.setInt(&acc.rsrp, &acc.hasRSRP, n)
				if acc.mode == "" {
					acc.mode = "4G"
				}
			}
		}
		if noSIM(cell.SIMStatus) {
			acc.connected = false
			acc.hasConn = true
		} else if v := strings.ToLower(asString(c["connection"])); v != "" {
			acc.connected = strings.Contains(v, "connected")
			acc.hasConn = true
		}
	}

	if acc.iface == "" {
		var ni struct {
			Interfaces []struct {
				ID string `json:"id"`
			} `json:"interfaces"`
		}
		if json.Unmarshal([]byte(netifRaw), &ni) == nil {
			for _, it := range ni.Interfaces {
				if isWWANName(it.ID) {
					acc.iface = it.ID
					break
				}
			}
		}
	}

	devPresent := false
	for _, line := range strings.Split(devsRaw, "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		if strings.HasPrefix(name, "/dev/cdc-wdm") {
			devPresent = true
			cell.Devices = appendUnique(cell.Devices, name)
			continue
		}
		if isWWANName(name) {
			devPresent = true
			cell.Devices = appendUnique(cell.Devices, name)
			if acc.iface == "" {
				acc.iface = name
			}
		}
	}

	cell.Mode = acc.mode
	cell.Operator = acc.operator
	cell.Interface = acc.iface
	if acc.hasRSRP {
		cell.RSRP = acc.rsrp
	}
	if acc.hasRSRQ {
		cell.RSRQ = acc.rsrq
	}
	if acc.hasSINR {
		cell.SINR = acc.sinr
	}
	if acc.hasRSSI {
		cell.RSSI = acc.rssi
	}
	if acc.hasConn {
		cell.Connected = acc.connected
		cell.Internet = acc.connected
	}
	cell.Present = cell.Count > 0 || cpePresent || devPresent || acc.hasRSRP
	if cell.Present && cell.Interface == "" {
		for _, sec := range sections {
			if v := strings.TrimSpace(secIface[sec]); v != "" {
				cell.Interface = v
				break
			}
		}
	}
	cell.Installed = cell.Installed || cell.Present
	if len(raws) > 0 {
		cell.Raw = strings.Join(raws, "\n")
	}
	if len(cell.Raw) > 4000 {
		cell.Raw = cell.Raw[:4000] + "\n..."
	}
	m.Modem = cell
	m.Caps.Cellular = cell.Present || cell.Installed
}

// modemScan 把 modem_ctrl 的 key/value 条目映射到统一的信号字段。
type modemScan struct {
	cell *CellularInfo
	acc  *modemSignals
}

func (p *modemScan) field(key string, val any) {
	k := strings.ToLower(strings.TrimSpace(key))
	k = strings.ReplaceAll(k, "_", " ")
	k = strings.Join(strings.Fields(k), " ")
	switch k {
	case "operator", "isp", "network operator", "carrier", "spn", "plmn":
		if p.acc.operator == "" {
			if s := strings.TrimSpace(asString(val)); s != "" && s != "00000" {
				p.acc.operator = s
			}
		}
	case "network type", "network mode", "mode", "rat", "act", "type":
		if p.acc.mode == "" {
			p.acc.mode = normalizeMode(asString(val))
		}
	case "rsrp", "lte rsrp", "nr rsrp", "5g rsrp":
		if n, ok := intValue(val); ok && n != 0 {
			p.acc.setInt(&p.acc.rsrp, &p.acc.hasRSRP, n)
		}
	case "rsrq", "lte rsrq", "nr rsrq":
		if n, ok := intValue(val); ok && n != 0 {
			p.acc.setInt(&p.acc.rsrq, &p.acc.hasRSRQ, n)
		}
	case "sinr", "snr", "lte sinr", "nr sinr":
		if n, ok := intValue(val); ok {
			p.acc.setInt(&p.acc.sinr, &p.acc.hasSINR, n)
		}
	case "rssi", "lte rssi", "nr rssi":
		if n, ok := intValue(val); ok && n != 0 {
			p.acc.setInt(&p.acc.rssi, &p.acc.hasRSSI, n)
		}
	case "sim status", "sim":
		if p.cell.SIMStatus == "" {
			p.cell.SIMStatus = strings.TrimSpace(asString(val))
		}
	case "model", "module", "name":
		if p.cell.Model == "" {
			p.cell.Model = strings.TrimSpace(asString(val))
		}
	case "band":
		if p.cell.Band == "" {
			p.cell.Band = strings.TrimSpace(asString(val))
		}
	case "interface", "iface", "network interface", "netdev":
		if p.acc.iface == "" && isWWANName(asString(val)) {
			p.acc.iface = asString(val)
		}
	}
}

var (
	qmodemSecRe = regexp.MustCompile(`^qmodem\.([^.=\s]+)=modem(-device)?$`)
	qmodemNetRe = regexp.MustCompile(`^qmodem\.([^.=\s]+)\.network=(.+)$`)
	modemIntRe  = regexp.MustCompile(`-?\d+`)
)

func parseQmodemSections(raw string) ([]string, map[string]string) {
	secs := []string{}
	nets := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if m := qmodemSecRe.FindStringSubmatch(line); m != nil {
			secs = append(secs, m[1])
			continue
		}
		if m := qmodemNetRe.FindStringSubmatch(line); m != nil {
			if v := strings.Trim(strings.TrimSpace(m[2]), "'\""); v != "" {
				nets[m[1]] = v
			}
		}
	}
	return secs, nets
}

func hasUbusObject(raw, name string) bool {
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) == name {
			return true
		}
	}
	return false
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(asString(m[k])); v != "" && v != "<nil>" {
			return v
		}
	}
	return ""
}

func intValue(v any) (int, bool) {
	switch t := v.(type) {
	case float64:
		return int(t), true
	case int:
		return t, true
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return int(n), true
		}
		return 0, false
	}
	m := modemIntRe.FindString(asString(v))
	if m == "" {
		return 0, false
	}
	n, err := strconv.Atoi(m)
	if err != nil {
		return 0, false
	}
	return n, true
}

func appendUnique(list []string, v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return list
	}
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func noSIM(s string) bool {
	u := strings.ToUpper(strings.TrimSpace(s))
	if u == "" {
		return false
	}
	// QModem Next 在 modem_ctrl info 里用 "miss" 表示未插卡，旧固件常见 ABSENT / NOT INSERTED。
	for _, bad := range []string{"ABSENT", "NOT INSERTED", "NO SIM", "MISS", "ERROR", "FAIL"} {
		if strings.Contains(u, bad) {
			return true
		}
	}
	return false
}

type modemSignals struct {
	rsrp, rsrq, sinr, rssi             int
	hasRSRP, hasRSRQ, hasSINR, hasRSSI bool
	mode, operator, iface              string
	connected                          bool
	hasConn                            bool
}

func (a *modemSignals) setInt(dst *int, has *bool, v any) {
	if *has {
		return
	}
	s := strings.TrimSpace(asString(v))
	if s == "" {
		return
	}
	n, err := strconv.Atoi(strings.Fields(s)[0])
	if err != nil {
		return
	}
	*dst = n
	*has = true
}

func scanModemSignals(v any, acc *modemSignals) {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			switch strings.ToLower(k) {
			case "rsrp", "rsrp_dbm":
				acc.setInt(&acc.rsrp, &acc.hasRSRP, val)
			case "rsrq", "rsrq_db":
				acc.setInt(&acc.rsrq, &acc.hasRSRQ, val)
			case "sinr", "sinr_db":
				acc.setInt(&acc.sinr, &acc.hasSINR, val)
			case "rssi", "rssi_dbm":
				acc.setInt(&acc.rssi, &acc.hasRSSI, val)
			case "network_mode", "net_mode", "network_type", "net_type", "mode", "act", "type":
				if acc.mode == "" {
					acc.mode = normalizeMode(asString(val))
				}
			case "operator", "operator_name", "network_operator", "carrier", "spn", "plmn":
				if acc.operator == "" {
					if s := strings.TrimSpace(asString(val)); s != "" && s != "00000" {
						acc.operator = s
					}
				}
			case "interface", "iface", "network_interface", "netdev", "device":
				if acc.iface == "" && isWWANName(asString(val)) {
					acc.iface = asString(val)
				}
			case "connected", "internet":
				if b, ok := val.(bool); ok {
					acc.connected = b
					acc.hasConn = true
				}
			case "up":
				if b, ok := val.(bool); ok && !acc.hasConn {
					acc.connected = b
				}
			case "status", "state", "connect_status", "dial_status", "registration":
				if acc.hasConn {
					break
				}
				s := strings.ToLower(asString(val))
				if strings.Contains(s, "connected") || strings.Contains(s, "online") || strings.Contains(s, "registered") || s == "up" {
					acc.connected = true
					acc.hasConn = true
				} else if strings.Contains(s, "disconnect") || strings.Contains(s, "down") || s == "idle" {
					acc.connected = false
					acc.hasConn = true
				}
			}
			scanModemSignals(val, acc)
		}
	case []any:
		for _, it := range x {
			scanModemSignals(it, acc)
		}
	}
}

func decodeJSONStream(s string) []any {
	out := []any{}
	dec := json.NewDecoder(strings.NewReader(s))
	for {
		var v any
		if err := dec.Decode(&v); err != nil {
			break
		}
		out = append(out, v)
	}
	return out
}

func normalizeMode(s string) string {
	u := strings.ToUpper(strings.TrimSpace(s))
	switch {
	case u == "":
		return ""
	case strings.Contains(u, "NR") || strings.Contains(u, "5G"):
		return "5G"
	case strings.Contains(u, "LTE") || strings.Contains(u, "4G"):
		return "4G"
	case strings.Contains(u, "WCDMA") || strings.Contains(u, "3G"):
		return "3G"
	case strings.Contains(u, "GSM") || strings.Contains(u, "2G"):
		return "2G"
	}
	return ""
}

func isWWANName(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, p := range []string{"wwan", "usb", "ppp", "rmnet"} {
		if strings.HasPrefix(s, p) {
			rest := strings.TrimPrefix(s, p)
			if rest == "" {
				return true
			}
			if _, err := strconv.Atoi(rest); err == nil {
				return true
			}
		}
	}
	return false
}

var mwanLineRe = regexp.MustCompile(`(?m)^\s*interface\s+(\S+)\s+is\s+(.+?)\s*$`)

func (m *Meta) parseMwan(text string) {
	m.MwanText = text
	info := MWANInfo{Installed: m.Caps.Mwan3, Running: m.Running["mwan3"]}
	for _, mt := range mwanLineRe.FindAllStringSubmatch(text, -1) {
		status := strings.TrimSpace(mt[2])
		status = strings.TrimSuffix(status, " ")
		info.Interfaces = append(info.Interfaces, MWANInterface{Name: mt[1], Status: status})
	}
	m.Mwan = info
}

func (m *Meta) connectionInfo(in ConnectionInput, fp string) *ConnectionInfo {
	model := m.Board.Model
	if model == "" {
		model = m.Board.BoardName
	}
	return &ConnectionInfo{
		Host:         in.Host,
		Port:         in.Port,
		User:         in.User,
		Fingerprint:  fp,
		Hostname:     m.Hostname,
		Model:        model,
		BoardName:    m.Board.BoardName,
		Chipset:      m.Chipset,
		Kernel:       m.Board.Kernel,
		Distribution: m.Board.Release["distribution"],
		Release:      m.Board.Release["description"],
		ConnectedAt:  time.Now().Unix(),
	}
}

func (m *Meta) hostByMAC() map[string]staticHost {
	out := map[string]staticHost{}
	for _, h := range m.Hosts {
		out[h.MAC] = h
	}
	return out
}

func (m *Meta) blockedByMAC() map[string]BlockRule {
	out := map[string]BlockRule{}
	for _, b := range m.Blocked {
		out[b.MAC] = b
	}
	return out
}

var macRe = regexp.MustCompile(`(?i)^[0-9a-f]{2}(:[0-9a-f]{2}){5}$`)

func normalizeMAC(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "-", ":")
	if !macRe.MatchString(s) {
		return ""
	}
	return s
}

var serviceCatalog = []struct {
	Key  string
	Name string
}{
	{"dnsmasq", "DNS / DHCP"},
	{"firewall", "防火墙"},
	{"dropbear", "SSH 服务"},
	{"qmodem", "4G 模组"},
	{"mwan3", "MWAN3 多线"},
	{"sqm", "SQM QoS"},
	{"AdGuardHome", "AdGuard Home"},
	{"adguardhome", "AdGuard Home"},
	{"https-dns-proxy", "DoH 代理"},
	{"passwall", "PassWall 代理"},
	{"shellcrash", "ShellCrash 代理"},
	{"openclash", "OpenClash 代理"},
	{"clash", "Clash 代理"},
	{"ttyd", "Web 终端"},
	{"ddns", "DDNS"},
}

func (m *Meta) serviceList() []ServiceStatus {
	seen := map[string]bool{}
	out := []ServiceStatus{}
	for _, sc := range serviceCatalog {
		if !m.Init[sc.Key] || seen[sc.Name] {
			continue
		}
		seen[sc.Name] = true
		out = append(out, ServiceStatus{
			Key:     sc.Key,
			Name:    sc.Name,
			Running: m.Running[sc.Key],
			Enabled: m.Enabled[sc.Key],
		})
	}
	return out
}
