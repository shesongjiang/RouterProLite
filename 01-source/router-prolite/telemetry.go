package main

import (
	"encoding/json"
	"math"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type cpuStat struct {
	total uint64
	idle  uint64
}

type netCounter struct {
	rx uint64
	tx uint64
}

type ubusSystemInfo struct {
	Uptime int64     `json:"uptime"`
	Load   []float64 `json:"load"`
	Memory struct {
		Total     int64 `json:"total"`
		Free      int64 `json:"free"`
		Available int64 `json:"available"`
	} `json:"memory"`
	Root struct {
		Total int64 `json:"total"`
		Free  int64 `json:"free"`
	} `json:"root"`
}

type ubusWANStatus struct {
	Up        bool   `json:"up"`
	Pending   bool   `json:"pending"`
	Available bool   `json:"available"`
	Proto     string `json:"proto"`
	Device    string `json:"device"`
	Data      struct {
		Uptime   int64  `json:"uptime"`
		L3Device string `json:"l3_device"`
		IPv4     []struct {
			Address string `json:"address"`
			Mask    int    `json:"mask"`
		} `json:"ipv4-address"`
		Gateway string   `json:"gateway"`
		DNS     []string `json:"dns-server"`
	} `json:"data"`
}

const snapshotScript = `echo '@@SYS'; ubus call system info 2>/dev/null
echo '@@STAT'; head -n1 /proc/stat 2>/dev/null
echo '@@CORES'; grep -c '^cpu[0-9]' /proc/stat 2>/dev/null
echo '@@MEM'; head -n3 /proc/meminfo 2>/dev/null
echo '@@LOAD'; cat /proc/loadavg 2>/dev/null
echo '@@UPTIME'; cut -d' ' -f1 /proc/uptime 2>/dev/null
echo '@@DEV'; cat /proc/net/dev 2>/dev/null
echo '@@WAN'; ubus call network.interface.wan status 2>/dev/null
echo '@@WAN6'; ubus call network.interface.wan6 status 2>/dev/null
echo '@@ADDR'; ip -4 -o addr show 2>/dev/null
echo '@@ROUTE'; ip -4 route show default 2>/dev/null
echo '@@LEASES'; cat /tmp/dhcp.leases 2>/dev/null
echo '@@ARP'; cat /proc/net/arp 2>/dev/null
echo '@@TEMP'; for f in /sys/class/thermal/thermal_zone*/temp; do [ -e "$f" ] && { cat "$f"; break; }; done
echo '@@SPEEDS'; for d in wan eth0 br-lan lan; do v=$(cat /sys/class/net/$d/speed 2>/dev/null); [ -n "$v" ] && echo "$d $v"; done
echo '@@LINKS'; for d in wan eth0 br-lan lan; do v=$(cat /sys/class/net/$d/operstate 2>/dev/null); [ -n "$v" ] && echo "$d $v"; done
echo '@@END'; true`

func parseCPUStat(line string) (cpuStat, bool) {
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuStat{}, false
	}
	var vals []uint64
	for _, f := range fields[1:] {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			v = 0
		}
		vals = append(vals, v)
	}
	var total uint64
	for _, v := range vals {
		total += v
	}
	idle := vals[3]
	if len(vals) > 4 {
		idle += vals[4]
	}
	return cpuStat{total: total, idle: idle}, true
}

func parseNetDev(out string) map[string]netCounter {
	res := map[string]netCounter{}
	for _, line := range strings.Split(out, "\n") {
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		name := strings.TrimSpace(line[:idx])
		if name == "" || strings.Contains(name, "|") {
			continue
		}
		fields := strings.Fields(line[idx+1:])
		if len(fields) < 9 {
			continue
		}
		rx, err1 := strconv.ParseUint(fields[0], 10, 64)
		tx, err2 := strconv.ParseUint(fields[8], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		res[name] = netCounter{rx: rx, tx: tx}
	}
	return res
}

type leaseEntry struct {
	expires int64
	mac     string
	ip      string
	name    string
}

func parseLeases(out string) []leaseEntry {
	var res []leaseEntry
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		exp, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			continue
		}
		mac := normalizeMAC(fields[1])
		if mac == "" || net.ParseIP(fields[2]) == nil {
			continue
		}
		name := ""
		if len(fields) > 3 && fields[3] != "*" {
			name = fields[3]
		}
		res = append(res, leaseEntry{expires: exp, mac: mac, ip: fields[2], name: name})
	}
	return res
}

type arpEntry struct {
	mac    string
	online bool
	device string
}

func parseARP(out string) map[string]arpEntry {
	res := map[string]arpEntry{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 || fields[0] == "IP" || strings.Contains(line, "HW type") {
			continue
		}
		if net.ParseIP(fields[0]) == nil {
			continue
		}
		mac := normalizeMAC(fields[3])
		if mac == "" || mac == "00:00:00:00:00:00" {
			continue
		}
		flags, _ := strconv.ParseInt(strings.TrimPrefix(fields[2], "0x"), 16, 64)
		res[fields[0]] = arpEntry{mac: mac, online: flags&0x2 != 0, device: fields[5]}
	}
	return res
}

func parseWAN(out, addrOut, routeOut string) (ubusWANStatus, string, string) {
	var w ubusWANStatus
	_ = json.Unmarshal([]byte(out), &w)
	ip, gw := "", ""
	if len(w.Data.IPv4) > 0 {
		ip = w.Data.IPv4[0].Address
	}
	gw = w.Data.Gateway
	dev := w.Device
	if w.Data.L3Device != "" {
		dev = w.Data.L3Device
	}
	if ip == "" || gw == "" {
		for _, line := range strings.Split(addrOut, "\n") {
			fields := strings.Fields(line)
			if len(fields) < 4 || fields[2] != "inet" {
				continue
			}
			iface := strings.TrimSuffix(fields[1], ":")
			if dev != "" && iface != dev {
				continue
			}
			if ip == "" {
				ip = strings.Split(fields[3], "/")[0]
			}
		}
	}
	if gw == "" {
		fields := strings.Fields(routeOut)
		for i, f := range fields {
			if f == "via" && i+1 < len(fields) {
				gw = fields[i+1]
				break
			}
		}
	}
	return w, ip, gw
}

func parseSpeeds(out string) map[string]string {
	res := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		speed, err := strconv.Atoi(fields[1])
		if err != nil || speed <= 0 {
			continue
		}
		if speed >= 1000 {
			res[fields[0]] = strconv.Itoa(speed) + "M FULL"
		} else {
			res[fields[0]] = strconv.Itoa(speed) + "M"
		}
	}
	return res
}

func parseLinks(out string) map[string]string {
	res := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		res[fields[0]] = fields[1]
	}
	return res
}

func parseAddrDevices(out string) map[string]bool {
	res := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[2] != "inet" {
			continue
		}
		res[strings.TrimSuffix(fields[1], ":")] = true
	}
	return res
}

func lanDeviceOf(meta *Meta, netNow map[string]netCounter) string {
	if sec := meta.UCI["network"]["lan"]; sec != nil && uciType(sec) == "interface" {
		for _, key := range []string{"device", "ifname"} {
			if fields := strings.Fields(asString(sec[key])); len(fields) > 0 {
				return fields[0]
			}
		}
	}
	if _, ok := netNow["br-lan"]; ok {
		return "br-lan"
	}
	return "lan"
}

func buildPorts(meta *Meta, wan, wan6 *WANInfo, rates *Rates, clients []Client, lanDev string, lanUp bool, speeds map[string]string) []PortStatus {
	online := 0
	for _, c := range clients {
		if c.Online {
			online++
		}
	}

	wanDetail := wan.Proto
	if wan.Device != "" {
		if wanDetail != "" {
			wanDetail += " · "
		}
		wanDetail += wan.Device
	}
	wanState := wan.Status
	if wanState == "" {
		wanState = "DOWN"
	}
	ports := []PortStatus{{
		Key:    "wan",
		Label:  "WAN",
		Up:     wan.Up,
		State:  wanState,
		Detail: wanDetail,
		Speed:  wan.LinkSpeed,
		Rx:     rates.WanRx,
		Tx:     rates.WanTx,
	}}

	wan6Configured := false
	if sec := meta.UCI["network"]["wan6"]; sec != nil && uciType(sec) == "interface" {
		wan6Configured = true
	}
	wan6State := "未配置"
	wan6Up := false
	wan6Detail := "IPv6 · wan6"
	if wan6Configured {
		wan6State = "DOWN"
		if wan6 != nil {
			wan6Up = wan6.Up
			if wan6.Status != "" {
				wan6State = wan6.Status
			}
			if wan6.Proto != "" {
				wan6Detail = "IPv6 · " + wan6.Proto
			}
		}
	}
	ports = append(ports, PortStatus{
		Key:    "wan6",
		Label:  "WAN6",
		Up:     wan6Up,
		State:  wan6State,
		Detail: wan6Detail,
	})

	lanState := "DOWN"
	if lanUp {
		lanState = "UP"
	}
	ports = append(ports, PortStatus{
		Key:     "lan",
		Label:   "LAN",
		Up:      lanUp,
		State:   lanState,
		Detail:  lanDev + " · " + strconv.Itoa(online) + " 台在线",
		Speed:   speeds[lanDev],
		Rx:      rates.LanRx,
		Tx:      rates.LanTx,
		Clients: online,
	})

	if meta.Caps.Wireless {
		wifi := meta.Wireless
		wifiUp := wifi.Present && wifi.Enabled
		wifiState := "无硬件"
		wifiDetail := "未检测到无线硬件"
		if wifi.Present {
			radios := len(wifi.Radios)
			if radios == 0 {
				radios = 1
			}
			wifiDetail = strconv.Itoa(radios) + " 射频"
			if wifi.Channel != "" {
				wifiDetail = "CH " + wifi.Channel + " · " + wifiDetail
			}
			if wifi.Enabled {
				wifiState = "UP"
			} else {
				wifiState = "OFF"
			}
		}
		ports = append(ports, PortStatus{
			Key:    "wifi",
			Label:  "WiFi",
			Up:     wifiUp,
			State:  wifiState,
			Detail: wifiDetail,
		})
	}

	if meta.Caps.Cellular {
		cellUp := meta.Modem.Present && meta.Modem.Internet
		cellState := "未连接"
		detail := "未检测到模组"
		if meta.Modem.Present {
			cellState = "DOWN"
			if cellUp {
				cellState = "UP"
			}
			if meta.Modem.Mode != "" {
				detail = meta.Modem.Mode
			} else {
				detail = "蜂窝网络"
			}
			if meta.Modem.Operator != "" {
				detail = meta.Modem.Operator
			}
			if meta.Modem.Interface != "" {
				detail += " · " + meta.Modem.Interface
			}
		} else {
			cellState = "无模组"
		}
		ports = append(ports, PortStatus{
			Key:    "cell",
			Label:  "4G/5G",
			Up:     cellUp,
			State:  cellState,
			Detail: detail,
		})
	}
	return ports
}

func parseMemInfo(out string) (total, available int64) {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		v, _ := strconv.ParseInt(fields[1], 10, 64)
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			total = v * 1024
		case "MemAvailable":
			available = v * 1024
		case "MemFree":
			if available == 0 {
				available = v * 1024
			}
		}
	}
	return
}

// resolveMemory 选择可信的内存总量与可用量。
// 部分旧固件的 ubus system info 只有 total/free，available 为 0；
// 此时依次回退到 free 与 /proc/meminfo，避免把已用内存误算成 100%。
func resolveMemory(total, available, free int64, memRaw string) (int64, int64) {
	if total <= 0 {
		return parseMemInfo(memRaw)
	}
	if available <= 0 {
		available = free
	}
	if available <= 0 {
		if _, a := parseMemInfo(memRaw); a > 0 {
			available = a
		}
	}
	return total, available
}

func ipLess(a, b string) bool {
	ia := net.ParseIP(a).To4()
	ib := net.ParseIP(b).To4()
	if ia != nil && ib != nil {
		for i := 0; i < 4; i++ {
			if ia[i] != ib[i] {
				return ia[i] < ib[i]
			}
		}
		return false
	}
	return a < b
}

func (r *Router) collectSnapshot(meta *Meta, info *ConnectionInfo) (*Snapshot, error) {
	out, err := r.exec(snapshotScript, 8*time.Second)
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, err
	}
	s := splitSections(out)
	now := time.Now()

	var sys ubusSystemInfo
	_ = json.Unmarshal([]byte(s["SYS"]), &sys)

	totalMem, availMem := resolveMemory(sys.Memory.Total, sys.Memory.Available, sys.Memory.Free, s["MEM"])
	uptime := sys.Uptime
	if uptime == 0 {
		if f, err := strconv.ParseFloat(strings.TrimSpace(s["UPTIME"]), 64); err == nil {
			uptime = int64(f)
		}
	}
	load1, load5, load15 := 0.0, 0.0, 0.0
	procs := 0
	if len(sys.Load) >= 3 {
		load1, load5, load15 = sys.Load[0]/65536.0, sys.Load[1]/65536.0, sys.Load[2]/65536.0
	} else if fields := strings.Fields(s["LOAD"]); len(fields) >= 3 {
		load1, _ = strconv.ParseFloat(fields[0], 64)
		load5, _ = strconv.ParseFloat(fields[1], 64)
		load15, _ = strconv.ParseFloat(fields[2], 64)
		if len(fields) > 3 {
			if parts := strings.Split(fields[3], "/"); len(parts) == 2 {
				procs, _ = strconv.Atoi(parts[1])
			}
		}
	}

	cores, _ := strconv.Atoi(strings.TrimSpace(s["CORES"]))
	if cores <= 0 {
		cores = 1
	}

	cpuPct := 0.0
	if stat, ok := parseCPUStat(strings.TrimSpace(s["STAT"])); ok {
		r.mu.Lock()
		prev := r.prevCPU
		prevAt := r.prevCPUTime
		r.prevCPU = stat
		r.prevCPUTime = now
		r.mu.Unlock()
		if !prevAt.IsZero() && stat.total > prev.total {
			dTotal := float64(stat.total - prev.total)
			dIdle := float64(stat.idle - prev.idle)
			cpuPct = math.Max(0, math.Min(100, (1-dIdle/dTotal)*100))
		}
	}

	temp := 0.0
	tempAvail := false
	if v, err := strconv.ParseFloat(strings.TrimSpace(s["TEMP"]), 64); err == nil && v > 0 {
		if v > 1000 {
			v /= 1000
		}
		if v > 0 && v < 150 {
			temp = v
			tempAvail = true
		}
	}

	wan, ip, gw := parseWAN(s["WAN"], s["ADDR"], s["ROUTE"])
	wanDevice := wan.Device
	if wan.Data.L3Device != "" {
		wanDevice = wan.Data.L3Device
	}
	if wanDevice == "" {
		wanDevice = meta.WANDevice
	}
	proto := wan.Proto
	if proto == "" {
		proto = meta.WANProto
	}
	wanStatus := "DOWN"
	switch {
	case wan.Up:
		wanStatus = "UP"
	case wan.Pending:
		wanStatus = "CONNECTING"
	case !wan.Available:
		wanStatus = "DISABLED"
	}

	netNow := parseNetDev(s["DEV"])
	r.mu.Lock()
	prevNet := r.prevNet
	prevNetAt := r.prevNetTime
	r.prevNet = netNow
	r.prevNetTime = now
	latencyMs, latencyOK := r.latencyMs, r.latencyOK
	r.mu.Unlock()

	if !wan.Up {
		latencyOK = false
	}
	lanDev := lanDeviceOf(meta, netNow)
	rates := Rates{LatencyMs: latencyMs, LatencyOK: latencyOK}
	if !prevNetAt.IsZero() {
		elapsed := now.Sub(prevNetAt).Seconds()
		if elapsed > 0.05 {
			if c, ok := netNow[wanDevice]; ok {
				if p, ok := prevNet[wanDevice]; ok {
					rates.WanRx = rateOf(c.rx, p.rx, elapsed)
					rates.WanTx = rateOf(c.tx, p.tx, elapsed)
				}
			}
			if c, ok := netNow[lanDev]; ok {
				if p, ok := prevNet[lanDev]; ok {
					rates.LanRx = rateOf(c.rx, p.rx, elapsed)
					rates.LanTx = rateOf(c.tx, p.tx, elapsed)
				}
			}
		}
	}

	speeds := parseSpeeds(s["SPEEDS"])
	wanInfo := WANInfo{
		Up:        wan.Up,
		Status:    wanStatus,
		Proto:     proto,
		Device:    wanDevice,
		IP:        ip,
		Gateway:   gw,
		DNS:       wan.Data.DNS,
		Uptime:    wan.Data.Uptime,
		LinkSpeed: speeds[wanDevice],
	}

	var wan6 ubusWANStatus
	_ = json.Unmarshal([]byte(s["WAN6"]), &wan6)
	wan6Status := "DOWN"
	switch {
	case wan6.Up:
		wan6Status = "UP"
	case wan6.Pending:
		wan6Status = "CONNECTING"
	case wan6.Proto == "" && wan6.Device == "":
		wan6Status = "DOWN"
	case !wan6.Available:
		wan6Status = "DISABLED"
	}
	wan6Info := WANInfo{Up: wan6.Up, Status: wan6Status, Proto: wan6.Proto, Device: wan6.Device}

	sysInfo := SystemInfo{
		Hostname:      meta.Hostname,
		Model:         info.Model,
		Chipset:       meta.Chipset,
		Kernel:        meta.Board.Kernel,
		Distribution:  meta.Board.Release["distribution"],
		Release:       meta.Board.Release["description"],
		Uptime:        uptime,
		CPUPct:        cpuPct,
		CPUCores:      cores,
		MemTotal:      totalMem,
		MemUsed:       totalMem - availMem,
		Load1:         load1,
		Load5:         load5,
		Load15:        load15,
		ProcessCount:  procs,
		TempC:         temp,
		TempAvailable: tempAvail,
		FlashTotal:    sys.Root.Total,
		FlashFree:     sys.Root.Free,
	}
	if totalMem > 0 {
		sysInfo.MemPct = math.Round(float64(sysInfo.MemUsed)/float64(totalMem)*1000) / 10
	}

	clients := buildClients(s["LEASES"], s["ARP"], meta)
	links := parseLinks(s["LINKS"])
	addrDevices := parseAddrDevices(s["ADDR"])
	lanUp := links[lanDev] == "up" || addrDevices[lanDev]
	for _, c := range clients {
		if c.Online {
			lanUp = true
			break
		}
	}
	ports := buildPorts(meta, &wanInfo, &wan6Info, &rates, clients, lanDev, lanUp, speeds)
	snap := &Snapshot{
		Connected:   true,
		RefreshedAt: now.UnixMilli(),
		Info:        info,
		System:      &sysInfo,
		WAN:         &wanInfo,
		Rates:       &rates,
		Clients:     clients,
		Ports:       ports,
		Plugins:     buildPluginSummaries(meta),
		Caps:        meta.Caps,
	}
	snap.Tiles = buildTiles(snap, meta)
	return snap, nil
}

func rateOf(cur, prev uint64, elapsed float64) float64 {
	if cur <= prev {
		return 0
	}
	return math.Round(float64(cur-prev)/elapsed*10) / 10
}

func buildClients(leasesRaw, arpRaw string, meta *Meta) []Client {
	now := time.Now().Unix()
	static := meta.hostByMAC()
	blocked := meta.blockedByMAC()
	leases := parseLeases(leasesRaw)
	arp := parseARP(arpRaw)

	byMAC := map[string]*Client{}
	for _, l := range leases {
		name := l.name
		if name == "" {
			if h, ok := static[l.mac]; ok {
				name = h.Name
			}
		}
		if name == "" {
			name = "未命名设备"
		}
		online := l.expires > now
		if a, ok := arp[l.ip]; ok && a.mac == l.mac && a.online {
			online = true
		}
		remaining := l.expires - now
		if remaining < 0 {
			remaining = 0
		}
		_, isStatic := static[l.mac]
		_, isBlocked := blocked[l.mac]
		byMAC[l.mac] = &Client{
			Name:           name,
			IP:             l.ip,
			MAC:            l.mac,
			Type:           "LAN",
			Online:         online,
			LeaseRemaining: remaining,
			ExpiresAt:      l.expires,
			Static:         isStatic,
			Blocked:        isBlocked,
		}
	}

	for ip, a := range arp {
		if _, ok := byMAC[a.mac]; ok {
			continue
		}
		if a.mac == "" || ip == "127.0.0.1" {
			continue
		}
		name := "静态/手工设备"
		if h, ok := static[a.mac]; ok && h.Name != "" {
			name = h.Name
		}
		_, isStatic := static[a.mac]
		_, isBlocked := blocked[a.mac]
		byMAC[a.mac] = &Client{
			Name:           name,
			IP:             ip,
			MAC:            a.mac,
			Type:           "LAN",
			Online:         a.online,
			LeaseRemaining: -1,
			Static:         isStatic,
			Blocked:        isBlocked,
		}
	}

	out := make([]Client, 0, len(byMAC))
	for _, c := range byMAC {
		out = append(out, *c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Online != out[j].Online {
			return out[i].Online
		}
		if out[i].Blocked != out[j].Blocked {
			return out[j].Blocked
		}
		return ipLess(out[i].IP, out[j].IP)
	})
	return out
}

var pingTimeRe = regexp.MustCompile(`time[=<]\s*([0-9.]+)\s*ms`)

func parsePingMS(out string) (float64, bool) {
	m := pingTimeRe.FindStringSubmatch(out)
	if len(m) < 2 {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// latencyTargets 返回延迟探测目标：优先国内可达的公共 DNS，最后回退到 WAN 网关。
func latencyTargets(gateway string) []string {
	targets := []string{"223.5.5.5", "1.1.1.1"}
	gw := strings.TrimSpace(gateway)
	if ip := net.ParseIP(gw); ip != nil && ip.To4() != nil {
		gw = ip.String()
		if gw != targets[0] && gw != targets[1] {
			targets = append(targets, gw)
		}
	}
	return targets
}

// latencyCommand 逐个尝试目标，首个成功即停止；输出保留成功的一次 ping 结果。
func latencyCommand(targets []string) string {
	quoted := make([]string, 0, len(targets))
	for _, t := range targets {
		quoted = append(quoted, shq(t))
	}
	return "for t in " + strings.Join(quoted, " ") +
		`; do o=$(ping -c1 -W1 "$t" 2>/dev/null); echo "$o"; case "$o" in *time=*) break;; esac; done`
}
