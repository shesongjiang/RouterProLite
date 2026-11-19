package main

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var (
	configNameRe = regexp.MustCompile(`^[A-Za-z0-9_]{1,32}$`)
	uciOptionRe  = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)
	ifaceNameRe  = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,32}$`)
	timezoneRe   = regexp.MustCompile(`^[A-Za-z0-9+\-:,_./]{1,64}$`)
	zoneNameRe   = regexp.MustCompile(`^[A-Za-z0-9_+\-/]{1,64}$`)
	leaseTimeRe  = regexp.MustCompile(`^(infinite|[0-9]{1,5}[smhd]?)$`)
	channelRe    = regexp.MustCompile(`^(auto|[0-9]{1,3})$`)
	widthRe      = regexp.MustCompile(`^((HT|VHT|HE|EHT)?(20|40|80|160|320))$`)
	txPowerRe    = regexp.MustCompile(`^(auto|[0-9]{1,2})$`)
	encryptRe    = regexp.MustCompile(`^(none|psk|psk2|psk-mixed|sae|sae-mixed|wpa|wpa2|wpa-mixed|wpa3|owe|owe-transition)$`)
	ntpHostRe    = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,253}$`)
)

func cleanUCIValue(s string, max int) (string, error) {
	s = strings.TrimSpace(s)
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' || (r < 32) || r == 127 {
			return "", errors.New("内容包含不允许的控制字符")
		}
	}
	if len([]rune(s)) > max {
		return "", fmt.Errorf("内容过长（最多 %d 个字符）", max)
	}
	return s, nil
}

func validateConfigID(config, section string) error {
	if !configNameRe.MatchString(config) {
		return errors.New("配置名不合法")
	}
	if !sectionRe.MatchString(section) {
		return errors.New("配置段 ID 不合法")
	}
	return nil
}

func (r *Router) GetConfigs() ([]string, error) {
	out, err := r.runChecked("ls /etc/config 2>/dev/null", 8*time.Second)
	if err != nil {
		return nil, fmt.Errorf("读取配置列表失败：%w", err)
	}
	seen := map[string]bool{}
	list := []string{}
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if !configNameRe.MatchString(name) || seen[name] {
			continue
		}
		seen[name] = true
		list = append(list, name)
	}
	sort.Strings(list)
	return list, nil
}

func (r *Router) GetConfigValues(config string) (map[string]map[string]any, error) {
	if !configNameRe.MatchString(config) {
		return nil, errors.New("配置名不合法")
	}
	cfg, err := r.uciGet(config)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return map[string]map[string]any{}, nil
	}
	return cfg, nil
}

func (r *Router) SetUCIOption(in UCIOptionInput) error {
	if err := validateConfigID(in.Config, in.Section); err != nil {
		return err
	}
	if !uciOptionRe.MatchString(in.Option) {
		return errors.New("选项名不合法")
	}
	value, err := cleanUCIValue(in.Value, 1024)
	if err != nil {
		return err
	}
	if err := r.uciCall("set", map[string]any{
		"config":  in.Config,
		"section": in.Section,
		"values":  map[string]any{in.Option: value},
	}); err != nil {
		return err
	}
	if err := r.commitConfig(in.Config); err != nil {
		return err
	}
	r.Invalidate()
	return nil
}

func (r *Router) SetUCIList(in UCIListInput) error {
	if err := validateConfigID(in.Config, in.Section); err != nil {
		return err
	}
	if !uciOptionRe.MatchString(in.Option) {
		return errors.New("选项名不合法")
	}
	if len(in.Values) > 16 {
		return errors.New("列表最多 16 项")
	}
	var b strings.Builder
	b.WriteString("uci -q delete " + in.Config + "." + in.Section + "." + in.Option + " 2>/dev/null\n")
	for _, item := range in.Values {
		value, err := cleanUCIValue(item, 512)
		if err != nil {
			return err
		}
		if value == "" {
			continue
		}
		b.WriteString("uci add_list " + in.Config + "." + in.Section + "." + in.Option + "=" + shq(value) + "\n")
	}
	b.WriteString("uci commit " + in.Config + "\n")
	b.WriteString("true")
	if _, err := r.runChecked(b.String(), 15*time.Second); err != nil {
		return fmt.Errorf("写入列表失败：%w", err)
	}
	r.Invalidate()
	return nil
}

func (r *Router) DeleteUCIOption(config, section, option string) error {
	if err := validateConfigID(config, section); err != nil {
		return err
	}
	if !uciOptionRe.MatchString(option) {
		return errors.New("选项名不合法")
	}
	script := "uci -q get " + config + "." + section + "." + option + " >/dev/null 2>&1 || { echo '选项不存在'; exit 1; }\n" +
		"uci delete " + config + "." + section + "." + option + "\n" +
		"uci commit " + config + "\n"
	if _, err := r.runChecked(script, 12*time.Second); err != nil {
		return fmt.Errorf("删除选项失败：%w", err)
	}
	r.Invalidate()
	return nil
}

func (r *Router) SetSystem(in SystemInput) error {
	if !sectionRe.MatchString(in.Section) {
		return errors.New("系统配置段不合法")
	}
	hostname := strings.TrimSpace(in.Hostname)
	if hostname != "" && !hostnameRe.MatchString(hostname) {
		return errors.New("主机名只能包含字母、数字、点、下划线或连字符")
	}
	timezone := strings.TrimSpace(in.Timezone)
	if timezone != "" && !timezoneRe.MatchString(timezone) {
		return errors.New("时区格式不正确（示例：CST-8）")
	}
	zoneName := strings.TrimSpace(in.ZoneName)
	if zoneName != "" && !zoneNameRe.MatchString(zoneName) {
		return errors.New("时区名称格式不正确（示例：Asia/Shanghai）")
	}
	servers := []string{}
	for _, s := range in.NTPServers {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !ntpHostRe.MatchString(s) {
			return fmt.Errorf("NTP 服务器 %q 不合法", s)
		}
		servers = append(servers, s)
	}
	if len(servers) > 4 {
		return errors.New("最多设置 4 个 NTP 服务器")
	}

	cfg, err := r.uciGet("system")
	if err != nil {
		return err
	}
	sec, ok := cfg[in.Section]
	if !ok || uciType(sec) != "system" {
		return errors.New("系统配置段不存在")
	}

	values := map[string]any{}
	if hostname != "" {
		values["hostname"] = hostname
	}
	if timezone != "" {
		values["timezone"] = timezone
	}
	if zoneName != "" {
		values["zonename"] = zoneName
	}
	if len(values) > 0 {
		if err := r.uciCall("set", map[string]any{"config": "system", "section": in.Section, "values": values}); err != nil {
			return err
		}
	}

	ntpSection := strings.TrimSpace(in.NTPSection)
	if ntpSection != "" {
		if !sectionRe.MatchString(ntpSection) {
			return errors.New("NTP 配置段不合法")
		}
		if err := r.uciCall("set", map[string]any{
			"config": "system", "section": ntpSection,
			"values": map[string]any{"enabled": "1"},
		}); err != nil {
			return err
		}
		var b strings.Builder
		b.WriteString("uci -q delete system." + ntpSection + ".server 2>/dev/null\n")
		for _, s := range servers {
			b.WriteString("uci add_list system." + ntpSection + ".server=" + shq(s) + "\n")
		}
		if _, err := r.runChecked(b.String()+"true", 10*time.Second); err != nil {
			return fmt.Errorf("写入 NTP 列表失败：%w", err)
		}
	}

	if err := r.commitConfig("system"); err != nil {
		return err
	}

	var apply strings.Builder
	if hostname != "" {
		apply.WriteString("hostname " + shq(hostname) + "\n")
	}
	apply.WriteString("[ -x /etc/init.d/system ] && /etc/init.d/system reload >/dev/null 2>&1\n")
	apply.WriteString("[ -x /etc/init.d/sysntpd ] && /etc/init.d/sysntpd restart >/dev/null 2>&1\n")
	apply.WriteString("[ -x /etc/init.d/led ] && /etc/init.d/led reload >/dev/null 2>&1\n")
	apply.WriteString("true")
	if _, err := r.runChecked(apply.String(), 15*time.Second); err != nil {
		return fmt.Errorf("配置已保存，但应用失败：%w", err)
	}
	r.Invalidate()
	return nil
}

// radioShouldDisable 判断保存某个 SSID 后，是否应同时停用整个射频。
// 只有该射频下所有 wifi-iface 都停用时才关射频，避免停用一个 SSID 连带其他 SSID 一起掉线。
func radioShouldDisable(cfg UCIConfig, radio, iface string, ifaceDisabled bool) bool {
	if radio == "" {
		return ifaceDisabled
	}
	found := false
	for id, sec := range cfg {
		if uciType(sec) != "wifi-iface" || asString(sec["device"]) != radio {
			continue
		}
		found = true
		disabled := asString(sec["disabled"]) == "1" || !uciEnabled(sec)
		if id == iface {
			disabled = ifaceDisabled
		}
		if !disabled {
			return false
		}
	}
	if !found {
		return ifaceDisabled
	}
	return true
}

func (r *Router) SetWiFi(in WiFiInput) error {
	if !sectionRe.MatchString(in.Section) {
		return errors.New("无线配置段不合法")
	}
	cfg, err := r.uciGet("wireless")
	if err != nil {
		return err
	}
	sec, ok := cfg[in.Section]
	if !ok || uciType(sec) != "wifi-iface" {
		return errors.New("无线接口不存在")
	}
	values := map[string]any{
		"disabled": map[bool]string{true: "1", false: "0"}[in.Disabled],
	}
	ssid := strings.TrimSpace(in.SSID)
	if ssid != "" {
		if len([]byte(ssid)) > 32 {
			return errors.New("SSID 最多 32 字节")
		}
		for _, r := range ssid {
			if r < 32 || r == 127 {
				return errors.New("SSID 包含非法字符")
			}
		}
		values["ssid"] = ssid
	}
	encryption := strings.TrimSpace(in.Encryption)
	if encryption != "" {
		if !encryptRe.MatchString(encryption) {
			return errors.New("不支持的加密方式")
		}
		values["encryption"] = encryption
	}
	key := in.Key
	if key != "" || encryption == "none" {
		if len(key) > 128 {
			return errors.New("无线密码过长")
		}
		for _, r := range key {
			if r < 32 || r == 127 {
				return errors.New("无线密码包含非法字符")
			}
		}
		values["key"] = key
	}
	if err := r.uciCall("set", map[string]any{"config": "wireless", "section": in.Section, "values": values}); err != nil {
		return err
	}

	if in.RadioSection != "" {
		if !sectionRe.MatchString(in.RadioSection) {
			return errors.New("射频配置段不合法")
		}
		rsec, ok := cfg[in.RadioSection]
		if !ok || uciType(rsec) != "wifi-device" {
			return errors.New("射频配置段不存在")
		}
		rvalues := map[string]any{
			"disabled": map[bool]string{true: "1", false: "0"}[radioShouldDisable(cfg, in.RadioSection, in.Section, in.Disabled)],
		}
		if channel := strings.TrimSpace(in.Channel); channel != "" {
			if !channelRe.MatchString(channel) {
				return errors.New("信道格式不正确")
			}
			rvalues["channel"] = channel
		}
		if width := strings.TrimSpace(in.Width); width != "" {
			if !widthRe.MatchString(width) {
				return errors.New("频宽格式不正确")
			}
			rvalues["htmode"] = width
		}
		if txPower := strings.TrimSpace(in.TxPower); txPower != "" {
			if !txPowerRe.MatchString(txPower) {
				return errors.New("发射功率格式不正确")
			}
			rvalues["txpower"] = txPower
		}
		if err := r.uciCall("set", map[string]any{"config": "wireless", "section": in.RadioSection, "values": rvalues}); err != nil {
			return err
		}
	}

	if err := r.commitConfig("wireless"); err != nil {
		return err
	}
	if _, err := r.runChecked("wifi reload 2>/dev/null || /etc/init.d/network reload >/dev/null 2>&1; true", 25*time.Second); err != nil {
		return fmt.Errorf("配置已保存，但无线重载失败：%w", err)
	}
	r.Invalidate()
	return nil
}

func (r *Router) SetDHCPPool(in DHCPPoolInput) error {
	if !sectionRe.MatchString(in.Section) {
		return errors.New("DHCP 配置段不合法")
	}
	cfg, err := r.uciGet("dhcp")
	if err != nil {
		return err
	}
	sec, ok := cfg[in.Section]
	if !ok {
		return errors.New("DHCP 配置段不存在")
	}
	if t := uciType(sec); t != "dhcp" && t != "dnsmasq" {
		return errors.New("该配置段不是 DHCP 池")
	}
	start := strings.TrimSpace(in.Start)
	if n, err := strconv.Atoi(start); err != nil || n < 0 || n > 250 {
		return errors.New("起始偏移需在 0-250 之间")
	}
	limit, err := strconv.Atoi(strings.TrimSpace(in.Limit))
	if err != nil || limit < 1 || limit > 1000 {
		return errors.New("地址池大小需在 1-1000 之间")
	}
	lease := strings.TrimSpace(in.LeaseTime)
	if !leaseTimeRe.MatchString(lease) {
		return errors.New("租期格式不正确（示例：12h、60m、infinite）")
	}
	values := map[string]any{"start": start, "limit": strconv.Itoa(limit), "leasetime": lease}
	if err := r.uciCall("set", map[string]any{"config": "dhcp", "section": in.Section, "values": values}); err != nil {
		return err
	}
	if err := r.commitConfig("dhcp"); err != nil {
		return err
	}
	if _, err := r.runChecked("/etc/init.d/dnsmasq restart >/dev/null 2>&1; true", 20*time.Second); err != nil {
		return fmt.Errorf("配置已保存，但重启 DHCP 失败：%w", err)
	}
	r.Invalidate()
	return nil
}

func (r *Router) SetUPnP(on bool) error {
	for _, name := range []string{"upnpd", "miniupnpd"} {
		cfg, err := r.uciGet(name)
		if err != nil || len(cfg) == 0 {
			continue
		}
		sectionID := ""
		for id, sec := range cfg {
			if _, ok := sec["enabled"]; ok {
				sectionID = id
				break
			}
			if sectionID == "" {
				sectionID = id
			}
		}
		if sectionID == "" || !sectionRe.MatchString(sectionID) {
			continue
		}
		value := "0"
		if on {
			value = "1"
		}
		if err := r.uciCall("set", map[string]any{"config": name, "section": sectionID, "values": map[string]any{"enabled": value}}); err != nil {
			return err
		}
		if err := r.commitConfig(name); err != nil {
			return err
		}
		initName := name
		if initName == "upnpd" {
			initName = "miniupnpd"
		}
		action := "restart"
		if !on {
			action = "stop"
		}
		script := "[ -x /etc/init.d/" + initName + " ] && /etc/init.d/" + initName + " " + action + " >/dev/null 2>&1\n" +
			"[ -x /etc/init.d/upnpd ] && /etc/init.d/upnpd " + action + " >/dev/null 2>&1\ntrue"
		if _, err := r.runChecked(script, 20*time.Second); err != nil {
			return fmt.Errorf("配置已保存，但重启 UPnP 失败：%w", err)
		}
		r.Invalidate()
		return nil
	}
	return errors.New("未找到 UPnP 配置（upnpd / miniupnpd）")
}

func (r *Router) InterfaceAction(name, action string) error {
	if !ifaceNameRe.MatchString(name) {
		return errors.New("接口名不合法")
	}
	switch action {
	case "up", "down", "renew":
	default:
		return errors.New("不支持的接口操作")
	}
	if _, err := r.runChecked("ubus call network.interface."+name+" "+action, 15*time.Second); err != nil {
		return fmt.Errorf("接口 %s 操作失败：%w", name, err)
	}
	r.Invalidate()
	return nil
}

var reloadServices = map[string]bool{
	"network": true, "firewall": true, "dnsmasq": true, "odhcpd": true,
	"system": true, "sqm": true, "wifi": true,
}

func init() {
	for _, def := range pluginCatalog {
		for _, name := range def.Init {
			reloadServices[name] = true
		}
	}
}

func (r *Router) ReloadService(name string) error {
	if !reloadServices[name] {
		return errors.New("不允许重载该服务")
	}
	var cmd string
	switch name {
	case "wifi":
		cmd = "wifi reload 2>/dev/null || /etc/init.d/network reload"
	case "network":
		cmd = "/etc/init.d/network reload"
	default:
		cmd = "/etc/init.d/" + name + " reload 2>/dev/null || /etc/init.d/" + name + " restart"
	}
	if _, err := r.runChecked(cmd, 30*time.Second); err != nil {
		return fmt.Errorf("重载 %s 失败：%w", name, err)
	}
	r.Invalidate()
	return nil
}

func (r *Router) Logs(kind, filter string, lines int) (string, error) {
	if lines <= 0 {
		lines = 200
	}
	if lines > 500 {
		lines = 500
	}
	filter = strings.TrimSpace(filter)
	if filter != "" {
		for _, ch := range filter {
			if !(unicode.IsLetter(ch) || unicode.IsDigit(ch) || strings.ContainsRune(" ._:/@-", ch)) {
				return "", errors.New("过滤词只能包含字母、数字和 . _ : / @ -")
			}
		}
		if len(filter) > 64 {
			return "", errors.New("过滤词过长")
		}
	}
	var base string
	switch kind {
	case "", "system":
		base = "logread"
	case "kernel":
		base = "dmesg"
	default:
		return "", errors.New("不支持的日志类型")
	}
	cmd := base + " 2>/dev/null"
	if filter != "" {
		if base == "logread" {
			cmd += " -e " + shq(filter)
		} else {
			cmd += " | grep -F " + shq(filter)
		}
	}
	cmd += " | tail -n " + strconv.Itoa(lines)
	out, code, err := r.runRaw(cmd, 15*time.Second)
	if err != nil && strings.TrimSpace(out) == "" {
		return "", err
	}
	if code != 0 && strings.TrimSpace(out) == "" {
		return "", errors.New("读取日志失败")
	}
	return out, nil
}
