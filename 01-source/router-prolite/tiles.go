package main

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

func buildTiles(s *Snapshot, m *Meta) []Tile {
	optional := []Tile{}

	if m.Caps.Wireless {
		status := "OFF"
		sub := "无线未启用"
		if m.Wireless.Enabled {
			status = "WIFI"
			sub = m.Wireless.SSID
			if m.Wireless.Channel != "" {
				sub += " · CH" + m.Wireless.Channel
			}
		}
		optional = append(optional, Tile{Key: "wifi", Title: "Wi-Fi", Status: status, Subtitle: sub, Tone: "blue", Active: m.Wireless.Enabled, Action: "section:network"})
	}
	if m.Caps.Cellular {
		tile := Tile{Key: "cellular", Title: "蜂窝网络", Action: "cell:detail"}
		if !m.Modem.Present {
			tile.Status = "无模组"
			tile.Subtitle = "未检测到模组"
			tile.Tone = "muted"
		} else {
			mode := m.Modem.Mode
			if mode == "" {
				mode = "蜂窝"
			}
			tile.Status = mode
			if m.Modem.SIMStatus != "" && noSIM(m.Modem.SIMStatus) {
				tile.Status = "未插卡"
			}
			switch {
			case m.Modem.SIMStatus != "" && noSIM(m.Modem.SIMStatus):
				tile.Subtitle = "未插 SIM 卡"
			case m.Modem.Operator != "":
				tile.Subtitle = m.Modem.Operator
			case m.Modem.Model != "":
				tile.Subtitle = m.Modem.Model
			default:
				tile.Subtitle = "已注册网络"
			}
			parts := []string{}
			if m.Modem.Connected {
				parts = append(parts, "已连接")
				tile.Tone = "green"
			} else {
				parts = append(parts, "未连接")
				tile.Tone = "amber"
			}
			if m.Modem.Interface != "" {
				parts = append(parts, m.Modem.Interface)
			}
			if m.Modem.RSRP != 0 {
				parts = append(parts, fmt.Sprintf("RSRP %d dBm", m.Modem.RSRP))
			}
			if m.Modem.SINR != 0 {
				parts = append(parts, fmt.Sprintf("SINR %d dB", m.Modem.SINR))
			}
			if len(parts) == 1 {
				parts = append(parts, "无信号数据")
			}
			tile.Detail = strings.Join(parts, " · ")
			tile.Active = m.Modem.Connected
		}
		optional = append(optional, tile)
	}
	if m.Caps.SQM {
		on := m.Running["sqm"]
		optional = append(optional, Tile{Key: "sqm", Title: "SQM QoS", Status: map[bool]string{true: "CAKE", false: "OFF"}[on], Subtitle: "抗缓冲膨胀 · 低抖动", Tone: "green", Active: on, Toggleable: true, On: on, Action: "toggle:sqm"})
	}
	if m.Caps.AdGuard {
		key := "AdGuardHome"
		if !m.Init[key] {
			key = "adguardhome"
		}
		on := m.Running[key]
		optional = append(optional, Tile{Key: "adguard", Title: "AdGuard DNS", Status: map[bool]string{true: "ON", false: "OFF"}[on], Subtitle: "DNS 过滤 · 去广告", Tone: "blue", Active: on, Toggleable: true, On: on, Action: "toggle:" + key})
	}
	if m.Caps.Proxy {
		key := ""
		for _, candidate := range []string{"passwall", "shellcrash", "openclash", "clash"} {
			if m.Init[candidate] {
				key = candidate
				break
			}
		}
		on := m.Running[key]
		optional = append(optional, Tile{Key: "proxy", Title: "Smart Proxy", Status: map[bool]string{true: "ON", false: "OFF"}[on], Subtitle: "透明代理 · 分流规则", Tone: "blue", Active: on, Toggleable: true, On: on, Action: "toggle:" + key})
	}
	if m.Caps.Mwan3 {
		status := "PAUSED"
		if m.Mwan.Running {
			status = "ACTIVE"
			for _, iface := range m.Mwan.Interfaces {
				if strings.Contains(iface.Status, "online") {
					status = "ONLINE"
					break
				}
			}
		}
		optional = append(optional, Tile{Key: "mwan3", Title: "MWAN3 多线", Status: status, Subtitle: fmt.Sprintf("%d 个接口 · 负载均衡", len(m.Mwan.Interfaces)), Tone: "amber", Active: m.Mwan.Running, Action: "section:network"})
	}

	core := []Tile{}
	wanStatus := "DOWN"
	wanTone := "muted"
	wanSub := m.WANProto + " · " + m.WANDevice
	if s.WAN != nil {
		wanStatus = "UP"
		wanTone = "green"
		if !s.WAN.Up {
			wanStatus = "DOWN"
			wanTone = "amber"
		}
		wanSub = s.WAN.Proto + " · " + s.WAN.Device
	}
	core = append(core, Tile{Key: "wan", Title: "WAN 链路", Status: wanStatus, Subtitle: wanSub, Tone: wanTone, Active: s.WAN != nil && s.WAN.Up, Action: "section:network"})

	fwStatus := "STD"
	if m.FullCone {
		fwStatus = "FULLCONE"
	}
	core = append(core, Tile{Key: "firewall", Title: "防火墙", Status: fwStatus, Subtitle: fmt.Sprintf("%d 端口映射 · %d 黑名单", len(m.Redirects), len(m.Blocked)), Tone: "blue", Active: m.FullCone, Action: "section:portforward"})

	dhcpSub := fmt.Sprintf("%d 台在线 · %d 静态", len(s.Clients), len(m.Hosts))
	dhcpStatus := "OFF"
	if m.Running["dnsmasq"] {
		dhcpStatus = "ON"
	}
	core = append(core, Tile{Key: "dhcp", Title: "DHCP 租约", Status: dhcpStatus, Subtitle: dhcpSub, Tone: "green", Active: dhcpStatus == "ON", Action: "clients"})

	// 核心磁贴（WAN / 防火墙 / DHCP）必须保留；能力磁贴只占用剩余名额。
	const maxTiles = 8
	if remain := maxTiles - len(core); len(optional) > remain {
		optional = optional[:remain]
	}
	return append(optional, core...)
}

func buildLab(m *Meta, snap *Snapshot, info *ConnectionInfo) *LabData {
	wan := WANInfo{Status: "DOWN", Proto: m.WANProto, Device: m.WANDevice}
	if snap != nil && snap.WAN != nil {
		wan = *snap.WAN
	}
	fp := ""
	if info != nil {
		fp = info.Fingerprint
	}
	return &LabData{
		Info:         info,
		Caps:         m.Caps,
		WAN:          wan,
		PortForwards: m.Redirects,
		DNS:          DNSSettings{Servers: m.DNS, Effective: m.Resolv, DoH: m.Caps.DoH},
		DDNS:         m.DDNS,
		FullCone:     m.FullCone,
		Services:     m.serviceList(),
		Cellular:     m.Modem,
		Mwan:         m.Mwan,
		Blocked:      m.Blocked,
		Wireless:     m.Wireless,
		Fingerprint:  fp,
	}
}

var errNotConnected = errors.New("尚未连接路由器")

func (r *Router) disconnectedSnapshot() Snapshot {
	r.mu.Lock()
	errText := r.lastErr
	r.mu.Unlock()
	return Snapshot{
		Connected:   false,
		Error:       errText,
		RefreshedAt: time.Now().UnixMilli(),
		Clients:     []Client{},
		Tiles:       []Tile{},
	}
}
