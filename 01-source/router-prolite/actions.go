package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var sectionRe = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)
var hostnameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)
var portRe = regexp.MustCompile(`^[0-9]{1,5}$`)
var portRangeRe = regexp.MustCompile(`^([0-9]{1,5})-([0-9]{1,5})$`)
var targetRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._:-]{0,252})$`)

func shq(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (r *Router) runRaw(cmd string, timeout time.Duration) (string, int, error) {
	client, err := r.ensureClient()
	if err != nil {
		return "", -1, err
	}
	return execWithClient(client, cmd, timeout)
}

func (r *Router) runChecked(cmd string, timeout time.Duration) (string, error) {
	out, code, err := r.runRaw(cmd, timeout)
	if err != nil {
		if code >= 0 {
			return out, fmt.Errorf("命令失败：%s", strings.TrimSpace(out))
		}
		return out, err
	}
	if code != 0 {
		msg := strings.TrimSpace(out)
		if msg == "" {
			msg = fmt.Sprintf("退出码 %d", code)
		}
		return out, errors.New(msg)
	}
	return out, nil
}

func (r *Router) uciGet(config string) (UCIConfig, error) {
	payload, _ := json.Marshal(map[string]string{"config": config})
	out, code, err := r.runRaw("ubus call uci get "+shq(string(payload)), 6*time.Second)
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("读取 %s 配置失败", config)
	}
	var resp struct {
		Values map[string]map[string]any `json:"values"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		return nil, fmt.Errorf("解析 %s 配置失败", config)
	}
	return resp.Values, nil
}

func (r *Router) uciCall(method string, payload map[string]any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	out, code, err := r.runRaw("ubus call uci "+method+" "+shq(string(data)), 8*time.Second)
	if err != nil {
		if code >= 0 {
			return fmt.Errorf("UCI %s 失败：%s", method, strings.TrimSpace(out))
		}
		return err
	}
	if code != 0 {
		return fmt.Errorf("UCI %s 失败：%s", method, strings.TrimSpace(out))
	}
	return nil
}

func (r *Router) commitConfig(config string) error {
	return r.uciCall("commit", map[string]any{"config": config})
}

func (r *Router) zoneName(fw UCIConfig, fallback, networkName string) string {
	for _, sec := range fw {
		if uciType(sec) != "zone" {
			continue
		}
		name := asString(sec["name"])
		if name == fallback {
			return name
		}
		for _, n := range asStrings(sec["network"]) {
			if n == networkName {
				return name
			}
		}
	}
	return fallback
}

func sanitizeLabel(s string, max int) string {
	s = strings.TrimSpace(s)
	s = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, s)
	if len(s) > max {
		s = s[:max]
	}
	return s
}

func validatePort(s string, allowRange bool) error {
	s = strings.TrimSpace(s)
	if allowRange {
		if m := portRangeRe.FindStringSubmatch(s); len(m) == 3 {
			lo, _ := strconv.Atoi(m[1])
			hi, _ := strconv.Atoi(m[2])
			if lo < 1 || hi > 65535 || lo > hi {
				return errors.New("端口范围需在 1-65535 之间且起始不大于结束")
			}
			return nil
		}
	}
	if !portRe.MatchString(s) {
		return errors.New("端口格式不正确")
	}
	p, _ := strconv.Atoi(s)
	if p < 1 || p > 65535 {
		return errors.New("端口需在 1-65535 之间")
	}
	return nil
}

func (r *Router) AddPortForward(in PortForwardInput) error {
	name := sanitizeLabel(in.Name, 48)
	proto := strings.ToLower(strings.TrimSpace(in.Proto))
	switch proto {
	case "tcp", "udp", "tcp/udp", "both", "tcp udp":
		if proto == "tcp/udp" || proto == "both" {
			proto = "tcp udp"
		}
	default:
		return errors.New("协议只支持 TCP 或 UDP")
	}
	extPort := strings.TrimSpace(in.ExtPort)
	destPort := strings.TrimSpace(in.DestPort)
	if err := validatePort(extPort, true); err != nil {
		return fmt.Errorf("外部端口：%w", err)
	}
	if err := validatePort(destPort, false); err != nil {
		return fmt.Errorf("内网端口：%w", err)
	}
	destIP := strings.TrimSpace(in.DestIP)
	ip := net.ParseIP(destIP)
	if ip == nil || ip.To4() == nil {
		return errors.New("内网 IP 必须是有效的 IPv4 地址")
	}
	if name == "" {
		name = "ProLite · " + extPort
	}

	fw, err := r.uciGet("firewall")
	if err != nil {
		return err
	}
	srcZone := r.zoneName(fw, "wan", "wan")
	destZone := r.zoneName(fw, "lan", "lan")

	values := map[string]any{
		"name":       name,
		"src":        srcZone,
		"dest":       destZone,
		"proto":      proto,
		"src_dport":  extPort,
		"dest_ip":    destIP,
		"dest_port":  destPort,
		"target":     "DNAT",
		"enabled":    "1",
		"reflection": "1",
	}
	if err := r.uciCall("add", map[string]any{"config": "firewall", "type": "redirect", "values": values}); err != nil {
		return err
	}
	if err := r.commitConfig("firewall"); err != nil {
		return err
	}
	if _, err := r.runChecked("/etc/init.d/firewall reload", 20*time.Second); err != nil {
		return fmt.Errorf("规则已保存，但防火墙重载失败：%w", err)
	}
	r.Invalidate()
	return nil
}

func (r *Router) TogglePortForward(id string, enabled bool) error {
	if !sectionRe.MatchString(id) {
		return errors.New("规则 ID 不合法")
	}
	fw, err := r.uciGet("firewall")
	if err != nil {
		return err
	}
	sec, ok := fw[id]
	if !ok || uciType(sec) != "redirect" {
		return errors.New("端口转发规则不存在")
	}
	value := "0"
	if enabled {
		value = "1"
	}
	if err := r.uciCall("set", map[string]any{"config": "firewall", "section": id, "values": map[string]any{"enabled": value}}); err != nil {
		return err
	}
	if err := r.commitConfig("firewall"); err != nil {
		return err
	}
	if _, err := r.runChecked("/etc/init.d/firewall reload", 20*time.Second); err != nil {
		return fmt.Errorf("已保存，但重载防火墙失败：%w", err)
	}
	r.Invalidate()
	return nil
}

func (r *Router) DeletePortForward(id string) error {
	if !sectionRe.MatchString(id) {
		return errors.New("规则 ID 不合法")
	}
	fw, err := r.uciGet("firewall")
	if err != nil {
		return err
	}
	sec, ok := fw[id]
	if !ok || uciType(sec) != "redirect" {
		return errors.New("端口转发规则不存在")
	}
	if err := r.uciCall("delete", map[string]any{"config": "firewall", "section": id}); err != nil {
		return err
	}
	if err := r.commitConfig("firewall"); err != nil {
		return err
	}
	if _, err := r.runChecked("/etc/init.d/firewall reload", 20*time.Second); err != nil {
		return fmt.Errorf("规则已删除，但重载防火墙失败：%w", err)
	}
	r.Invalidate()
	return nil
}

func (r *Router) SetStaticLease(in StaticLeaseInput) error {
	mac := normalizeMAC(in.MAC)
	if mac == "" {
		return errors.New("MAC 地址格式不正确")
	}
	ip := strings.TrimSpace(in.IP)
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.To4() == nil {
		return errors.New("IP 地址格式不正确")
	}
	name := strings.TrimSpace(in.Name)
	if name != "" && !hostnameRe.MatchString(name) {
		return errors.New("主机名只能包含字母、数字、点、下划线或连字符")
	}

	dhcp, err := r.uciGet("dhcp")
	if err != nil {
		return err
	}
	var matches []string
	for id, sec := range dhcp {
		if uciType(sec) != "host" {
			continue
		}
		if normalizeMAC(asString(sec["mac"])) == mac {
			matches = append(matches, id)
		}
	}

	if in.Enabled {
		values := map[string]any{"ip": ip, "mac": mac}
		if name != "" {
			values["name"] = name
		}
		if len(matches) > 0 {
			if err := r.uciCall("set", map[string]any{"config": "dhcp", "section": matches[0], "values": values}); err != nil {
				return err
			}
			for _, id := range matches[1:] {
				_ = r.uciCall("delete", map[string]any{"config": "dhcp", "section": id})
			}
		} else {
			if err := r.uciCall("add", map[string]any{"config": "dhcp", "type": "host", "values": values}); err != nil {
				return err
			}
		}
	} else {
		if len(matches) == 0 {
			return nil
		}
		for _, id := range matches {
			if err := r.uciCall("delete", map[string]any{"config": "dhcp", "section": id}); err != nil {
				return err
			}
		}
	}

	if err := r.commitConfig("dhcp"); err != nil {
		return err
	}
	if _, err := r.runChecked("/etc/init.d/dnsmasq restart", 20*time.Second); err != nil {
		return fmt.Errorf("已保存，但重启 DHCP 失败：%w", err)
	}
	r.Invalidate()
	return nil
}

func (r *Router) SetBlocked(macRaw string, blocked bool) error {
	mac := normalizeMAC(macRaw)
	if mac == "" {
		return errors.New("MAC 地址格式不正确")
	}
	name := "prolite-block-" + mac
	fw, err := r.uciGet("firewall")
	if err != nil {
		return err
	}
	var matches []string
	for id, sec := range fw {
		if uciType(sec) != "rule" {
			continue
		}
		if normalizeMAC(asString(sec["src_mac"])) == mac && strings.HasPrefix(asString(sec["name"]), "prolite-block-") {
			matches = append(matches, id)
		}
	}

	if blocked {
		if len(matches) == 0 {
			values := map[string]any{
				"name":    name,
				"src":     "lan",
				"src_mac": mac,
				"proto":   "all",
				"target":  "DROP",
				"enabled": "1",
			}
			if err := r.uciCall("add", map[string]any{"config": "firewall", "type": "rule", "values": values}); err != nil {
				return err
			}
		}
	} else {
		if len(matches) == 0 {
			return nil
		}
		for _, id := range matches {
			if err := r.uciCall("delete", map[string]any{"config": "firewall", "section": id}); err != nil {
				return err
			}
		}
	}

	if err := r.commitConfig("firewall"); err != nil {
		return err
	}
	if _, err := r.runChecked("/etc/init.d/firewall reload", 20*time.Second); err != nil {
		return fmt.Errorf("已保存，但重载防火墙失败：%w", err)
	}
	r.Invalidate()
	return nil
}

func (r *Router) SetDNS(servers string) error {
	var cleaned []string
	seen := map[string]bool{}
	for _, part := range strings.FieldsFunc(servers, func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		ip := net.ParseIP(part)
		if ip == nil {
			return fmt.Errorf("DNS 地址 %q 不合法", part)
		}
		if !seen[part] {
			seen[part] = true
			cleaned = append(cleaned, part)
		}
	}
	if len(cleaned) > 4 {
		return errors.New("最多设置 4 个 DNS 服务器")
	}

	dhcp, err := r.uciGet("dhcp")
	if err != nil {
		return err
	}
	sectionID := ""
	for id, sec := range dhcp {
		if uciType(sec) == "dnsmasq" {
			sectionID = id
			break
		}
	}
	if sectionID == "" || !sectionRe.MatchString(sectionID) {
		return errors.New("未找到 dnsmasq 配置段")
	}

	var b strings.Builder
	b.WriteString("uci -q delete dhcp." + sectionID + ".server 2>/dev/null\n")
	for _, ip := range cleaned {
		b.WriteString("uci add_list dhcp." + sectionID + ".server=" + shq(ip) + "\n")
	}
	b.WriteString("uci commit dhcp\n")
	b.WriteString("/etc/init.d/dnsmasq restart\n")
	if _, err := r.runChecked(b.String(), 25*time.Second); err != nil {
		return fmt.Errorf("保存 DNS 失败：%w", err)
	}
	r.Invalidate()
	return nil
}

func (r *Router) SetFullCone(on bool) error {
	fw, err := r.uciGet("firewall")
	if err != nil {
		return err
	}
	sectionID := ""
	for id, sec := range fw {
		if uciType(sec) == "defaults" {
			sectionID = id
			break
		}
	}
	if sectionID == "" {
		return errors.New("未找到防火墙默认配置段")
	}
	value := "0"
	if on {
		value = "1"
	}
	if err := r.uciCall("set", map[string]any{"config": "firewall", "section": sectionID, "values": map[string]any{"fullcone": value}}); err != nil {
		return err
	}
	if err := r.commitConfig("firewall"); err != nil {
		return err
	}
	if _, err := r.runChecked("/etc/init.d/firewall reload", 20*time.Second); err != nil {
		return fmt.Errorf("已保存，但重载防火墙失败：%w", err)
	}
	r.Invalidate()
	return nil
}

var allowedServices = map[string]bool{
	"sqm":             true,
	"AdGuardHome":     true,
	"adguardhome":     true,
	"https-dns-proxy": true,
	"passwall":        true,
	"passwall2":       true,
	"shellcrash":      true,
	"openclash":       true,
	"clash":           true,
	"mihomo":          true,
	"mosdns":          true,
	"smartdns":        true,
	"unbound":         true,
	"tailscale":       true,
	"zerotier":        true,
	"openvpn":         true,
	"frpc":            true,
	"nps":             true,
	"alist":           true,
	"aria2":           true,
	"transmission":    true,
	"qbittorrent":     true,
	"dockerd":         true,
	"samba4":          true,
	"vlmcsd":          true,
	"netdata":         true,
	"nginx":           true,
	"ttyd":            true,
	"miniupnpd":       true,
	"upnpd":           true,
	"mwan3":           true,
}

func (r *Router) ToggleService(name string, on bool) error {
	if !allowedServices[name] {
		return errors.New("不允许操作该服务")
	}
	action := "start"
	if on {
		action = "enable"
	} else {
		action = "disable"
	}
	script := "/etc/init.d/" + name + " " + action + " >/dev/null 2>&1\n"
	if on {
		script += "/etc/init.d/" + name + " restart >/dev/null 2>&1\n"
	} else {
		script += "/etc/init.d/" + name + " stop >/dev/null 2>&1\n"
	}
	script += "true"
	if _, err := r.runChecked(script, 30*time.Second); err != nil {
		return fmt.Errorf("切换服务失败：%w", err)
	}
	r.Invalidate()
	return nil
}

func (r *Router) WANAction(action string) error {
	switch action {
	case "up", "down", "renew":
	default:
		return errors.New("不支持的操作")
	}
	if _, err := r.runChecked("ubus call network.interface.wan "+action, 15*time.Second); err != nil {
		return fmt.Errorf("WAN 操作失败：%w", err)
	}
	r.Invalidate()
	return nil
}

func (r *Router) ReloadNetwork() error {
	if _, err := r.runChecked("/etc/init.d/network reload", 25*time.Second); err != nil {
		return fmt.Errorf("重载网络失败：%w", err)
	}
	r.Invalidate()
	return nil
}

func (r *Router) Reboot() error {
	out, code, err := r.runRaw("reboot", 6*time.Second)
	// reboot 正常会让 SSH 连接直接断开并返回连接错误，这里只上报能确认的执行失败
	if err == nil && code != 0 {
		return fmt.Errorf("重启命令失败（退出码 %d）：%s", code, strings.TrimSpace(out))
	}
	return nil
}

func validateTarget(target string) error {
	if !targetRe.MatchString(target) || strings.Contains(target, "..") {
		return errors.New("目标地址格式不正确")
	}
	return nil
}
