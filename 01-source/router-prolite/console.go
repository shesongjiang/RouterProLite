package main

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const consoleScript = `echo '@@SYSBOARD'; ubus call system board 2>/dev/null
echo '@@SYSTEMUCI'; ubus call uci get '{"config":"system"}' 2>/dev/null
echo '@@DATE'; date '+%Y-%m-%d %H:%M:%S %Z' 2>/dev/null
echo '@@UNAME'; uname -a 2>/dev/null
echo '@@MACHINE'; uname -m 2>/dev/null
echo '@@CPUINFO'; sed -n '1,80p' /proc/cpuinfo 2>/dev/null
echo '@@CPUFREQ'; cat /sys/devices/system/cpu/cpu0/cpufreq/scaling_cur_freq 2>/dev/null; dmesg 2>/dev/null | sed -n 's/.*sched_clock: 64 bits at \([0-9]*\)MHz.*/\1/p' | head -n1
echo '@@MEMINFO'; cat /proc/meminfo 2>/dev/null
echo '@@UPTIME'; cut -d' ' -f1 /proc/uptime 2>/dev/null
echo '@@DF'; df -h 2>/dev/null
echo '@@MOUNTS'; cat /proc/mounts 2>/dev/null
echo '@@IFACES'; ubus call network.interface dump 2>/dev/null
echo '@@DEV'; cat /proc/net/dev 2>/dev/null
echo '@@SPEEDS'; for d in /sys/class/net/*; do n=${d##*/}; s=$(cat "$d/speed" 2>/dev/null); [ -n "$s" ] && echo "$n $s"; done
echo '@@IWINFO'; iwinfo 2>/dev/null; for d in $(iwinfo 2>/dev/null | awk 'NR>1{print $1}'); do echo "##DEV $d"; iwinfo "$d" info 2>/dev/null; echo "##ASSOC $d"; iwinfo "$d" assoclist 2>/dev/null | wc -l; done
echo '@@PKGS'; if command -v apk >/dev/null 2>&1; then apk info -v 2>/dev/null; else opkg list-installed 2>/dev/null; fi
echo '@@INITD'; ls /etc/init.d 2>/dev/null
echo '@@CONFIGS'; ls /etc/config 2>/dev/null
echo '@@UPNP'; ubus call uci get '{"config":"upnpd"}' 2>/dev/null
echo '@@UPNP2'; ubus call uci get '{"config":"miniupnpd"}' 2>/dev/null
echo '@@LEASES'; cat /tmp/dhcp.leases 2>/dev/null
echo '@@PS'; ps w 2>/dev/null | head -n 90
echo '@@MODEM'; ubus call qmodem info 2>/dev/null
echo '@@MODEMCTRL'; ubus call modem_ctrl info 2>/dev/null
echo '@@QMODEM_NETIF'; ubus call qmodem get_network_interfaces 2>/dev/null
echo '@@QMODEM_SECS'; for s in $(uci show qmodem 2>/dev/null | sed -n -e 's/^qmodem\.\([^.=]*\)=modem$/\1/p' -e 's/^qmodem\.\([^.=]*\)=modem-device$/\1/p'); do echo "##SEC $s"; ubus call qmodem base_info "{\"config_section\":\"$s\"}" 2>/dev/null; ubus call qmodem cell_info "{\"config_section\":\"$s\"}" 2>/dev/null; ubus call qmodem network_info "{\"config_section\":\"$s\"}" 2>/dev/null; ubus call qmodem dial_status "{\"config_section\":\"$s\"}" 2>/dev/null; ubus call qmodem get_connect_status "{\"config_section\":\"$s\"}" 2>/dev/null; done
echo '@@QMODEM_UCI'; uci -q show qmodem 2>/dev/null
echo '@@MODEMDEVS'; ls /sys/class/net 2>/dev/null | grep -E '^(wwan|rmnet)' 2>/dev/null; ls /dev/cdc-wdm* 2>/dev/null
echo '@@UBUSMODEM'; ubus list 2>/dev/null | grep -E '^(qmodem|modem_ctrl|cpe_network)$'
echo '@@CPE'; ubus call cpe_network status 2>/dev/null
echo '@@MWAN'; mwan3 status 2>/dev/null
echo '@@END'; true`

type pluginDef struct {
	Key      string
	Name     string
	Config   string
	Init     []string
	Packages []string
}

var pluginCatalog = []pluginDef{
	{"passwall", "PassWall", "passwall", []string{"passwall"}, []string{"luci-app-passwall", "passwall"}},
	{"passwall2", "PassWall 2", "passwall2", []string{"passwall2"}, []string{"luci-app-passwall2", "passwall2"}},
	{"openclash", "OpenClash", "openclash", []string{"openclash"}, []string{"luci-app-openclash", "openclash"}},
	{"shellcrash", "ShellCrash", "shellcrash", []string{"shellcrash"}, []string{"shellcrash"}},
	{"mihomo", "Mihomo", "mihomo", []string{"mihomo"}, []string{"luci-app-mihomo", "mihomo"}},
	{"clash", "Clash", "clash", []string{"clash"}, []string{"luci-app-clash", "clash"}},
	{"ssrplus", "SSR Plus", "shadowsocksr", []string{"shadowsocksr"}, []string{"luci-app-ssr-plus"}},
	{"adguard", "AdGuard Home", "AdGuardHome", []string{"AdGuardHome", "adguardhome"}, []string{"luci-app-adguardhome", "adguardhome", "AdGuardHome"}},
	{"mosdns", "MosDNS", "mosdns", []string{"mosdns"}, []string{"luci-app-mosdns", "mosdns"}},
	{"smartdns", "SmartDNS", "smartdns", []string{"smartdns"}, []string{"luci-app-smartdns", "smartdns"}},
	{"doh", "DoH 代理", "https-dns-proxy", []string{"https-dns-proxy"}, []string{"luci-app-https-dns-proxy", "https-dns-proxy"}},
	{"unbound", "Unbound DNS", "unbound", []string{"unbound"}, []string{"luci-app-unbound", "unbound"}},
	{"mwan3", "MWAN3 多线", "mwan3", []string{"mwan3"}, []string{"luci-app-mwan3", "mwan3"}},
	{"sqm", "SQM QoS", "sqm", []string{"sqm"}, []string{"luci-app-sqm", "sqm-scripts"}},
	{"ddns", "DDNS", "ddns", []string{"ddns"}, []string{"luci-app-ddns", "ddns-scripts"}},
	{"upnp", "UPnP", "upnpd", []string{"miniupnpd", "upnpd"}, []string{"luci-app-upnp", "miniupnpd"}},
	{"tailscale", "Tailscale", "tailscale", []string{"tailscale"}, []string{"luci-app-tailscale", "tailscale"}},
	{"zerotier", "ZeroTier", "zerotier", []string{"zerotier"}, []string{"luci-app-zerotier", "zerotier"}},
	{"openvpn", "OpenVPN", "openvpn", []string{"openvpn"}, []string{"luci-app-openvpn", "openvpn-openssl", "openvpn"}},
	{"wireguard", "WireGuard", "network", nil, []string{"luci-proto-wireguard", "kmod-wireguard", "wireguard-tools"}},
	{"frpc", "FRP 客户端", "frpc", []string{"frpc"}, []string{"luci-app-frpc", "frpc"}},
	{"nps", "NPS 穿透", "nps", []string{"nps"}, []string{"luci-app-nps", "nps"}},
	{"alist", "AList", "alist", []string{"alist"}, []string{"luci-app-alist", "alist"}},
	{"aria2", "Aria2", "aria2", []string{"aria2"}, []string{"luci-app-aria2", "aria2"}},
	{"transmission", "Transmission", "transmission", []string{"transmission"}, []string{"luci-app-transmission", "transmission-daemon"}},
	{"qbittorrent", "qBittorrent", "qbittorrent", []string{"qbittorrent"}, []string{"luci-app-qbittorrent", "qbittorrent"}},
	{"docker", "Docker", "dockerd", []string{"dockerd"}, []string{"luci-app-dockerman", "dockerd", "docker"}},
	{"qmodem", "4G 模组", "qmodem", []string{"qmodem"}, []string{"luci-app-qmodem", "qmodem"}},
	{"ttyd", "Web 终端", "ttyd", []string{"ttyd"}, []string{"luci-app-ttyd", "ttyd"}},
	{"samba", "Samba", "samba4", []string{"samba4"}, []string{"luci-app-samba4", "samba4-server"}},
	{"vlmcsd", "KMS 激活", "vlmcsd", []string{"vlmcsd"}, []string{"luci-app-vlmcsd", "vlmcsd"}},
	{"netdata", "Netdata", "netdata", []string{"netdata"}, []string{"luci-app-netdata", "netdata"}},
	{"nginx", "Nginx", "nginx", []string{"nginx"}, []string{"luci-app-nginx", "nginx"}},
	{"uhttpd", "uHTTPd", "uhttpd", []string{"uhttpd"}, []string{"uhttpd"}},
}

func buildPluginSummaries(m *Meta) []PluginSummary {
	out := []PluginSummary{}
	for _, def := range pluginCatalog {
		initName := ""
		enabled := false
		running := false
		for _, name := range def.Init {
			if m.Init[name] {
				if initName == "" {
					initName = name
				}
				enabled = enabled || m.Enabled[name]
				running = running || m.Running[name]
			}
		}
		if initName == "" {
			continue
		}
		out = append(out, PluginSummary{Key: def.Key, Name: def.Name, Running: running, Enabled: enabled})
	}
	return out
}

var apkPkgRe = regexp.MustCompile(`^(.+?)-([0-9][^ ]*)$`)

func parsePackages(raw string) []PackageInfo {
	out := []PackageInfo{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, version := line, ""
		if idx := strings.Index(line, " - "); idx > 0 {
			name = strings.TrimSpace(line[:idx])
			version = strings.TrimSpace(line[idx+3:])
		} else if m := apkPkgRe.FindStringSubmatch(line); m != nil {
			name, version = m[1], m[2]
		} else if fields := strings.Fields(line); len(fields) >= 2 {
			name, version = fields[0], fields[1]
		}
		if name == "" {
			continue
		}
		out = append(out, PackageInfo{Name: name, Version: version})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func parseProcessList(raw string) []ProcessInfo {
	out := []ProcessInfo{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] == "PID" {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		vsz, _ := strconv.ParseInt(fields[2], 10, 64)
		out = append(out, ProcessInfo{
			PID:     pid,
			User:    fields[1],
			VSZ:     vsz,
			Stat:    fields[3],
			Command: strings.Join(fields[4:], " "),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].VSZ > out[j].VSZ })
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}

func parseDF(raw string) []StorageInfo {
	out := []StorageInfo{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 || fields[0] == "Filesystem" || strings.HasPrefix(fields[0], "-----") {
			continue
		}
		out = append(out, StorageInfo{
			Filesystem: fields[0],
			Size:       fields[1],
			Used:       fields[2],
			Available:  fields[3],
			UsePct:     fields[4],
			Mount:      strings.Join(fields[5:], " "),
		})
	}
	if len(out) > 16 {
		out = out[:16]
	}
	return out
}

var mountFS = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true, "f2fs": true, "btrfs": true, "xfs": true,
	"vfat": true, "exfat": true, "ntfs": true, "ntfs3": true, "fuseblk": true,
	"overlay": true, "squashfs": true, "jffs2": true, "ubifs": true, "nfs": true,
	"nfs4": true, "cifs": true, "smb3": true, "iso9660": true,
}

func parseMounts(raw string) []MountInfo {
	out := []MountInfo{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		mount := fields[1]
		if !(mount == "/" || strings.HasPrefix(mount, "/overlay") || strings.HasPrefix(mount, "/mnt") ||
			strings.HasPrefix(mount, "/usb") || strings.HasPrefix(mount, "/boot") ||
			strings.HasPrefix(mount, "/tmp") || mountFS[fields[2]]) {
			continue
		}
		out = append(out, MountInfo{Device: fields[0], Mount: mount, FS: fields[2], Options: fields[3]})
	}
	if len(out) > 30 {
		out = out[:30]
	}
	return out
}

type ifaceRoute struct {
	Target  string `json:"target"`
	Mask    int    `json:"mask"`
	Gateway string `json:"gateway"`
	Source  string `json:"source"`
}

type dumpIPv4 struct {
	Address string `json:"address"`
	Mask    int    `json:"mask"`
}

type dumpIface struct {
	Interface string         `json:"interface"`
	Up        bool           `json:"up"`
	Pending   bool           `json:"pending"`
	Available bool           `json:"available"`
	Proto     string         `json:"proto"`
	Device    string         `json:"device"`
	L3Device  string         `json:"l3_device"`
	Uptime    int64          `json:"uptime"`
	IPv4      []dumpIPv4     `json:"ipv4-address"`
	DNS       []string       `json:"dns-server"`
	Route     []ifaceRoute   `json:"route"`
	Data      map[string]any `json:"data"`
}

func jstr(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	return asString(m[key])
}

func jint(m map[string]any, key string) int64 {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	}
	return 0
}

func jv4(m map[string]any, key string) []string {
	if m == nil {
		return nil
	}
	raw, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := []string{}
	for _, item := range raw {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		addr := asString(obj["address"])
		if addr == "" {
			continue
		}
		if mask := jint(obj, "mask"); mask > 0 {
			addr += "/" + strconv.FormatInt(mask, 10)
		}
		out = append(out, addr)
	}
	return out
}

func parseInterfaces(raw string, counters map[string]netCounter, speeds map[string]string) []InterfaceInfo {
	var dump struct {
		Interface []dumpIface `json:"interface"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &dump); err != nil {
		return nil
	}
	out := make([]InterfaceInfo, 0, len(dump.Interface))
	for _, it := range dump.Interface {
		dev := it.Device
		if it.L3Device != "" {
			dev = it.L3Device
		}
		ips := []string{}
		for _, v4 := range it.IPv4 {
			addr := v4.Address
			if addr == "" {
				continue
			}
			if v4.Mask > 0 {
				addr += "/" + strconv.Itoa(v4.Mask)
			}
			ips = append(ips, addr)
		}
		if len(ips) == 0 {
			ips = jv4(it.Data, "ipv4-address")
		}
		gw := jstr(it.Data, "gateway")
		if gw == "" {
			for _, r := range it.Route {
				if r.Target == "0.0.0.0" && r.Gateway != "" {
					gw = r.Gateway
					break
				}
			}
		}
		dns := it.DNS
		if len(dns) == 0 {
			dns = asStrings(it.Data["dns-server"])
		}
		uptime := it.Uptime
		if uptime == 0 {
			uptime = jint(it.Data, "uptime")
		}
		info := InterfaceInfo{
			Name:      it.Interface,
			Up:        it.Up,
			Pending:   it.Pending,
			Available: it.Available,
			Proto:     it.Proto,
			Device:    dev,
			IPs:       ips,
			Gateway:   gw,
			DNS:       dns,
			Uptime:    uptime,
			LinkSpeed: speeds[dev],
		}
		if c, ok := counters[dev]; ok {
			info.RxBytes = c.rx
			info.TxBytes = c.tx
		}
		out = append(out, info)
	}
	return out
}

type iwInfo struct {
	SSID    string
	Channel string
	Band    string
	TxPower string
	Signal  string
	Bitrate string
	Clients int
}

func parseIwinfo(raw string) map[string]*iwInfo {
	out := map[string]*iwInfo{}
	var cur *iwInfo
	var assocDev string
	expectCount := false
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "##DEV ") {
			dev := strings.TrimSpace(strings.TrimPrefix(trimmed, "##DEV "))
			cur = &iwInfo{}
			out[dev] = cur
			assocDev = ""
			expectCount = false
			continue
		}
		if strings.HasPrefix(trimmed, "##ASSOC ") {
			assocDev = strings.TrimSpace(strings.TrimPrefix(trimmed, "##ASSOC "))
			expectCount = true
			cur = nil
			continue
		}
		if expectCount {
			if trimmed == "" {
				continue
			}
			if n, err := strconv.Atoi(trimmed); err == nil {
				if info := out[assocDev]; info != nil {
					info.Clients = n
				}
			}
			expectCount = false
			continue
		}
		if cur == nil {
			continue
		}
		if idx := strings.Index(trimmed, "ESSID:"); idx >= 0 {
			if cur.SSID == "" {
				cur.SSID = strings.Trim(strings.TrimSpace(trimmed[idx+6:]), `"`)
			}
		}
		if idx := strings.Index(trimmed, "Channel:"); idx >= 0 {
			rest := strings.TrimSpace(trimmed[idx+8:])
			if fields := strings.Fields(rest); len(fields) > 0 && cur.Channel == "" {
				cur.Channel = fields[0]
			}
			if open := strings.Index(rest, "("); open >= 0 {
				freq := rest[open:]
				switch {
				case strings.Contains(freq, "2."):
					cur.Band = "2.4G"
				case strings.Contains(freq, "5."):
					cur.Band = "5G"
				case strings.Contains(freq, "6."):
					cur.Band = "6G"
				}
			}
		}
		if idx := strings.Index(trimmed, "Tx-Power:"); idx >= 0 {
			cur.TxPower = strings.TrimSpace(trimmed[idx+9:])
		}
		if idx := strings.Index(trimmed, "Signal:"); idx >= 0 {
			rest := strings.TrimSpace(trimmed[idx+7:])
			if fields := strings.Fields(rest); len(fields) > 0 {
				cur.Signal = fields[0] + " dBm"
			}
		}
		if idx := strings.Index(trimmed, "Bit Rate:"); idx >= 0 {
			cur.Bitrate = strings.TrimSpace(trimmed[idx+9:])
		}
	}
	return out
}

func mapWidth(htmode string) string {
	for i := len(htmode) - 1; i >= 0; i-- {
		if htmode[i] < '0' || htmode[i] > '9' {
			if i == len(htmode)-1 {
				return ""
			}
			return htmode[i+1:]
		}
	}
	return ""
}

func parseWireless(meta *Meta, iw map[string]*iwInfo) WirelessDetail {
	out := WirelessDetail{Present: meta.Caps.Wireless}
	cfg := meta.UCI["wireless"]
	radioActive := map[string]bool{}
	for id, sec := range cfg {
		if uciType(sec) != "wifi-device" {
			continue
		}
		disabled := asString(sec["disabled"]) == "1" || !uciEnabled(sec)
		band := asString(sec["band"])
		if band == "" {
			switch asString(sec["hwmode"]) {
			case "11a", "11ac", "11axa":
				band = "5G"
			case "11b", "11g", "11ng", "11axg":
				band = "2.4G"
			}
		}
		radio := WiFiRadio{
			Section:  id,
			Name:     id,
			Band:     strings.ToUpper(band),
			Channel:  asString(sec["channel"]),
			Width:    mapWidth(asString(sec["htmode"])),
			TxPower:  asString(sec["txpower"]),
			Country:  asString(sec["country"]),
			Path:     asString(sec["path"]),
			Disabled: disabled,
			Active:   !disabled,
		}
		radioActive[id] = !disabled
		out.Radios = append(out.Radios, radio)
	}
	used := map[string]bool{}
	for id, sec := range cfg {
		if uciType(sec) != "wifi-iface" {
			continue
		}
		disabled := asString(sec["disabled"]) == "1" || !uciEnabled(sec)
		key := asString(sec["key"])
		if key == "" {
			key = asString(sec["psk"])
		}
		if key == "" {
			key = asString(sec["password"])
		}
		if key == "" {
			key = asString(sec["sae_password"])
		}
		iface := WiFiIface{
			Section:    id,
			Radio:      asString(sec["device"]),
			Mode:       asString(sec["mode"]),
			SSID:       asString(sec["ssid"]),
			Network:    asString(sec["network"]),
			Encryption: asString(sec["encryption"]),
			Key:        key,
			Hidden:     asString(sec["hidden"]) == "1",
			Disabled:   disabled,
			Active:     !disabled && radioActive[asString(sec["device"])],
		}
		for dev, info := range iw {
			if used[dev] || info.SSID == "" || info.SSID != iface.SSID {
				continue
			}
			iface.Clients = info.Clients
			iface.Signal = info.Signal
			iface.Bitrate = info.Bitrate
			if iface.Channel == "" {
				iface.Channel = info.Channel
			}
			used[dev] = true
			break
		}
		out.Ifaces = append(out.Ifaces, iface)
	}
	sort.SliceStable(out.Radios, func(i, j int) bool { return out.Radios[i].Section < out.Radios[j].Section })
	sort.SliceStable(out.Ifaces, func(i, j int) bool { return out.Ifaces[i].Section < out.Ifaces[j].Section })
	return out
}

func parseDHCPDetail(meta *Meta, leases []leaseEntry) DHCPDetail {
	out := DHCPDetail{Pools: []DHCPPool{}, StaticHosts: []StaticHost{}, Leases: []LeaseView{}}
	for id, sec := range meta.UCI["dhcp"] {
		switch uciType(sec) {
		case "dhcp", "dnsmasq":
			ignore := !uciEnabled(sec)
			if raw, ok := sec["ignore"]; ok {
				v := asString(raw)
				ignore = v != "" && v != "0" && v != "false" && v != "no"
			}
			out.Pools = append(out.Pools, DHCPPool{
				Section:   id,
				Start:     asString(sec["start"]),
				Limit:     asString(sec["limit"]),
				LeaseTime: asString(sec["leasetime"]),
				Interface: asString(sec["interface"]),
				Ignore:    ignore,
			})
		case "host":
			mac := normalizeMAC(asString(sec["mac"]))
			if mac == "" {
				continue
			}
			out.StaticHosts = append(out.StaticHosts, StaticHost{
				Section: id,
				Name:    asString(sec["name"]),
				IP:      asString(sec["ip"]),
				MAC:     mac,
			})
		}
	}
	sort.SliceStable(out.Pools, func(i, j int) bool {
		return indexOf(meta.UCI["dhcp"][out.Pools[i].Section]) < indexOf(meta.UCI["dhcp"][out.Pools[j].Section])
	})
	sort.SliceStable(out.StaticHosts, func(i, j int) bool { return ipLess(out.StaticHosts[i].IP, out.StaticHosts[j].IP) })
	for _, l := range leases {
		out.Leases = append(out.Leases, LeaseView{Hostname: l.name, IP: l.ip, MAC: l.mac, Expires: l.expires})
	}
	return out
}

func parseUPnP(meta *Meta, raw, raw2 string) (bool, bool, bool, string, string) {
	var cfg UCIConfig
	name := ""
	if parsed := parseUCI(raw); len(parsed) > 0 {
		cfg, name = parsed, "upnpd"
	} else if parsed := parseUCI(raw2); len(parsed) > 0 {
		cfg, name = parsed, "miniupnpd"
	}
	if len(cfg) == 0 {
		installed := meta.Init["miniupnpd"] || meta.Init["upnpd"]
		running := meta.Running["miniupnpd"] || meta.Running["upnpd"]
		return installed, running, false, "", ""
	}
	enabled := false
	sectionID := ""
	for id, sec := range cfg {
		if v, ok := sec["enabled"]; ok {
			enabled = asString(v) != "0"
			sectionID = id
			break
		}
		if sectionID == "" {
			sectionID = id
		}
	}
	running := meta.Running["miniupnpd"] || meta.Running["upnpd"]
	installed := true
	return installed, running, enabled, sectionID, name
}

func parseFirewallDetail(meta *Meta, upnpInstalled, upnpRunning, upnpEnabled bool, upnpSection, upnpName string) FirewallDetail {
	out := FirewallDetail{
		Zones:          []FirewallZone{},
		UPnPInstalled:  upnpInstalled,
		UPnPRunning:    upnpRunning,
		UPnPEnabled:    upnpEnabled,
		UPnPSection:    upnpSection,
		UPnPConfigName: upnpName,
	}
	for _, sec := range meta.UCI["firewall"] {
		switch uciType(sec) {
		case "zone":
			out.Zones = append(out.Zones, FirewallZone{
				Name:     asString(sec["name"]),
				Networks: asStrings(sec["network"]),
				Input:    asString(sec["input"]),
				Output:   asString(sec["output"]),
				Forward:  asString(sec["forward"]),
				Masq:     asString(sec["masq"]) == "1",
			})
		case "rule":
			out.RuleCount++
		case "redirect":
			out.RedirectCount++
		case "defaults":
			if v, ok := sec["fullcone"]; ok && asString(v) == "1" {
				out.FullCone = true
			}
		}
	}
	sort.SliceStable(out.Zones, func(i, j int) bool {
		ai, aj := out.Zones[i].Name, out.Zones[j].Name
		if ai == "lan" {
			return true
		}
		if aj == "lan" {
			return false
		}
		return ai < aj
	})
	return out
}

func parseSystemSettings(raw string) (section, ntpSection, hostname, timezone, zonename string, ntpEnabled bool, servers []string) {
	cfg := parseUCI(raw)
	ntpEnabled = true
	for id, sec := range cfg {
		switch uciType(sec) {
		case "system":
			if section == "" || asString(sec["hostname"]) != "" {
				section = id
				hostname = asString(sec["hostname"])
				timezone = asString(sec["timezone"])
				zonename = asString(sec["zonename"])
			}
		case "ntp":
			ntpSection = id
			if v, ok := sec["enabled"]; ok {
				ntpEnabled = asString(v) != "0"
			}
			servers = asStrings(sec["server"])
		}
	}
	return
}

func parseConsole(out string, meta *Meta) *ConsoleData {
	s := splitSections(out)
	c := &ConsoleData{
		Board:      meta.Board,
		Release:    meta.Board.Release,
		Hostname:   meta.Hostname,
		Kernel:     meta.Board.Kernel,
		FetchedAt:  time.Now().UnixMilli(),
		Interfaces: []InterfaceInfo{},
		Plugins:    []PluginInfo{},
		Packages:   []PackageInfo{},
		Processes:  []ProcessInfo{},
		Storage:    []StorageInfo{},
		Mounts:     []MountInfo{},
		Configs:    []string{},
	}
	if c.Release == nil {
		c.Release = map[string]string{}
	}
	var board BoardInfo
	if err := json.Unmarshal([]byte(s["SYSBOARD"]), &board); err == nil && board.Model != "" {
		c.Board = board
	}
	c.Uname = strings.TrimSpace(s["UNAME"])
	c.Arch = strings.TrimSpace(s["MACHINE"])
	if c.Arch == "" {
		fields := strings.Fields(c.Uname)
		if len(fields) >= 2 {
			c.Arch = fields[len(fields)-2]
		}
	}

	cpuInfo := s["CPUINFO"]
	c.CPUCores, c.CPUModel = parseCPUInfo(cpuInfo)
	if c.CPUCores == 0 {
		c.CPUCores = 1
	}
	if values := strings.Fields(s["CPUFREQ"]); len(values) > 0 {
		if v, err := strconv.ParseInt(values[0], 10, 64); err == nil && v > 0 {
			if v < 10000 {
				c.CPUFreqMHz = int(v)
			} else {
				c.CPUFreqMHz = int(v / 1000)
			}
		}
	}

	c.Memory = parseMemInfoFull(s["MEMINFO"])
	if f, err := strconv.ParseFloat(strings.TrimSpace(s["UPTIME"]), 64); err == nil {
		c.Uptime = int64(f)
	}
	c.Date = strings.TrimSpace(s["DATE"])

	c.Storage = parseDF(s["DF"])
	c.Mounts = parseMounts(s["MOUNTS"])

	counters := parseNetDev(s["DEV"])
	speeds := parseSpeeds(s["SPEEDS"])
	c.Interfaces = parseInterfaces(s["IFACES"], counters, speeds)

	byName := map[string]string{}
	for _, p := range parsePackages(s["PKGS"]) {
		byName[p.Name] = p.Version
		c.Packages = append(c.Packages, p)
	}
	c.PackageCount = len(c.Packages)
	configSet := map[string]bool{}
	for _, line := range strings.Split(s["CONFIGS"], "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		c.Configs = append(c.Configs, name)
		configSet[name] = true
	}
	c.Plugins = detectPlugins(meta, byName, configSet)

	iw := parseIwinfo(s["IWINFO"])
	c.Wireless = parseWireless(meta, iw)

	leases := parseLeases(s["LEASES"])
	c.DHCP = parseDHCPDetail(meta, leases)

	upnpInstalled, upnpRunning, upnpEnabled, upnpSection, upnpName := parseUPnP(meta, s["UPNP"], s["UPNP2"])
	c.Firewall = parseFirewallDetail(meta, upnpInstalled, upnpRunning, upnpEnabled, upnpSection, upnpName)

	c.SystemSec, c.NTPSection, _, c.Timezone, c.ZoneName, c.NTPEnabled, c.NTPServers =
		parseSystemSettings(s["SYSTEMUCI"])
	if v := asString(parseUCI(s["SYSTEMUCI"])[c.SystemSec]["hostname"]); v != "" {
		c.Hostname = v
	}

	c.Processes = parseProcessList(s["PS"])

	tmp := &Meta{}
	tmp.parseModem(s)
	c.Modem = tmp.Modem
	mcopy := *meta
	mcopy.parseMwan(s["MWAN"])
	c.Mwan = mcopy.Mwan

	return c
}

func parseCPUInfo(raw string) (int, string) {
	cores := 0
	model := ""
	declared := 0
	for _, line := range strings.Split(raw, "\n") {
		low := strings.ToLower(line)
		if strings.HasPrefix(low, "processor") && strings.Contains(line, ":") {
			cores++
		}
		if strings.Contains(low, "cpu cores") && strings.Contains(line, ":") {
			if v, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(line, ":", 2)[1])); err == nil {
				declared = v
			}
		}
		if model != "" {
			continue
		}
		for _, key := range []string{"model name", "system type", "cpu model", "hardware"} {
			if strings.HasPrefix(low, key) && strings.Contains(line, ":") {
				val := strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
				if val != "" && !strings.HasPrefix(strings.ToLower(val), "arm") {
					model = val
				}
				break
			}
		}
	}
	if cores == 0 {
		cores = declared
	}
	return cores, model
}

func parseMemInfoFull(raw string) MemoryInfo {
	var m MemoryInfo
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		v, _ := strconv.ParseInt(fields[1], 10, 64)
		v *= 1024
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			m.Total = v
		case "MemFree":
			m.Free = v
		case "MemAvailable":
			m.Available = v
		case "Buffers":
			m.Buffers = v
		case "Cached":
			m.Cached = v
		case "SwapTotal":
			m.SwapTotal = v
		case "SwapFree":
			m.SwapFree = v
		}
	}
	if m.Available == 0 {
		m.Available = m.Free
	}
	return m
}

func detectPlugins(meta *Meta, pkgs map[string]string, configs map[string]bool) []PluginInfo {
	out := []PluginInfo{}
	for _, def := range pluginCatalog {
		initName := ""
		enabled := false
		running := false
		for _, name := range def.Init {
			if meta.Init[name] {
				if initName == "" {
					initName = name
				}
				enabled = enabled || meta.Enabled[name]
				running = running || meta.Running[name]
			}
		}
		matchedPkg := ""
		for _, name := range def.Packages {
			if _, ok := pkgs[name]; ok {
				matchedPkg = name
				break
			}
		}
		hasConfig := def.Config != "" && configs[def.Config]
		if initName == "" && matchedPkg == "" && !hasConfig {
			continue
		}
		out = append(out, PluginInfo{
			Key:       def.Key,
			Name:      def.Name,
			Version:   pkgs[matchedPkg],
			Package:   matchedPkg,
			Config:    def.Config,
			Init:      initName,
			Installed: true,
			Enabled:   enabled,
			Running:   running,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Running != out[j].Running {
			return out[i].Running
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (r *Router) Console(force bool) (*ConsoleData, error) {
	r.mu.Lock()
	meta := r.meta
	metaAt := r.metaAt
	cached := r.console
	cachedAt := r.consoleAt
	r.mu.Unlock()
	if !force && cached != nil && time.Since(cachedAt) < 20*time.Second {
		return cached, nil
	}
	if meta == nil || force || time.Since(metaAt) > 20*time.Second {
		if m, err := r.fetchMeta(); err == nil {
			meta = m
		} else if meta == nil {
			return nil, err
		}
	}
	out, err := r.exec(consoleScript, 25*time.Second)
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, err
	}
	data := parseConsole(out, meta)
	r.mu.Lock()
	r.console = data
	r.consoleAt = time.Now()
	r.mu.Unlock()
	return data, nil
}
