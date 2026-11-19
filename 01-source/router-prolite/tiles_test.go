package main

import (
	"strings"
	"testing"
)

func TestBuildTilesKeepsCoreTiles(t *testing.T) {
	snap := &Snapshot{
		WAN:     &WANInfo{Up: true, Proto: "dhcp", Device: "wan", Gateway: "192.168.1.1"},
		Clients: []Client{{IP: "192.168.1.10", Online: true}},
	}
	meta := &Meta{
		Caps:     Capabilities{Wireless: true, Cellular: true, SQM: true, AdGuard: true, Proxy: true, Mwan3: true},
		Wireless: WirelessInfo{Present: true, Enabled: true, SSID: "test", Channel: "6"},
		Modem: CellularInfo{Present: true, Connected: true, Internet: true, Count: 1,
			Mode: "5G", Operator: "CHINA MOBILE", Interface: "wwan0", RSRP: -85, SINR: 12},
		Init:    map[string]bool{"adguardhome": true, "passwall": true},
		Running: map[string]bool{"sqm": true, "adguardhome": true, "passwall": true, "dnsmasq": true},
		Mwan:    MWANInfo{Installed: true, Running: true},
	}
	tiles := buildTiles(snap, meta)
	if len(tiles) > 8 {
		t.Fatalf("tile count %d exceeds cap", len(tiles))
	}
	byKey := map[string]Tile{}
	for _, tile := range tiles {
		byKey[tile.Key] = tile
	}
	for _, want := range []string{"wan", "firewall", "dhcp"} {
		if _, ok := byKey[want]; !ok {
			t.Fatalf("core tile %q dropped: %+v", want, byKey)
		}
	}
	cell, ok := byKey["cellular"]
	if !ok {
		t.Fatalf("cellular tile missing: %+v", byKey)
	}
	if cell.Status != "5G" || cell.Subtitle != "CHINA MOBILE" {
		t.Fatalf("cellular tile fields: %+v", cell)
	}
	for _, want := range []string{"已连接", "wwan0", "RSRP -85 dBm", "SINR 12 dB"} {
		if !strings.Contains(cell.Detail, want) {
			t.Fatalf("cellular detail %q missing %q", cell.Detail, want)
		}
	}
	if !cell.Active {
		t.Fatalf("cellular should be active: %+v", cell)
	}
}

func TestCellularTileWithoutModem(t *testing.T) {
	tiles := buildTiles(&Snapshot{}, &Meta{
		Caps:    Capabilities{Cellular: true},
		Modem:   CellularInfo{Installed: true, Present: false},
		Init:    map[string]bool{"qmodem": true},
		Running: map[string]bool{},
	})
	byKey := map[string]Tile{}
	for _, tile := range tiles {
		byKey[tile.Key] = tile
	}
	cell, ok := byKey["cellular"]
	if !ok {
		t.Fatalf("cellular tile should still exist when qmodem is installed")
	}
	if cell.Status != "无模组" || cell.Subtitle != "未检测到模组" || cell.Detail != "" {
		t.Fatalf("no-modem tile must not show fake values: %+v", cell)
	}
	if _, ok := byKey["wifi"]; ok {
		t.Fatalf("wifi tile must not occupy a slot without wireless hardware")
	}
}

func TestNoCellularStackNoTile(t *testing.T) {
	tiles := buildTiles(&Snapshot{}, &Meta{
		Init:    map[string]bool{},
		Running: map[string]bool{},
	})
	for _, tile := range tiles {
		if tile.Key == "cellular" || tile.Key == "wifi" {
			t.Fatalf("unexpected radio tile without hardware/stack: %+v", tile)
		}
	}
}

func TestBuildTilesWithoutCapabilities(t *testing.T) {
	tiles := buildTiles(&Snapshot{}, &Meta{
		Init:    map[string]bool{},
		Running: map[string]bool{},
	})
	if len(tiles) != 3 {
		t.Fatalf("expected only core tiles, got %d", len(tiles))
	}
}

func TestParseModemSections(t *testing.T) {
	m := &Meta{Init: map[string]bool{"qmodem": true}}
	secs := `##SEC modem1
{"config_section":"modem1","device":"/dev/ttyUSB2"}
{"cell_info":[{"type":"LTE","rsrp":"-85","sinr":"12","rsrq":"-10"}]}
{"operator":"CHINA MOBILE","interface":"wwan0","connected":true}
`
	m.parseModem(`{"error":"explicit valid modem section required"}`, `{"interfaces":{},"cellular":{}}`, secs, `{"interfaces":[{"id":"wan"}]}`)
	if !m.Modem.Present || !m.Modem.Installed || m.Modem.Count != 1 {
		t.Fatalf("modem presence: %+v", m.Modem)
	}
	if m.Modem.Mode != "4G" || m.Modem.Operator != "CHINA MOBILE" || m.Modem.Interface != "wwan0" {
		t.Fatalf("modem identity: %+v", m.Modem)
	}
	if m.Modem.RSRP != -85 || m.Modem.SINR != 12 || m.Modem.RSRQ != -10 || !m.Modem.Connected {
		t.Fatalf("modem signal: %+v", m.Modem)
	}
	if !m.Caps.Cellular {
		t.Fatalf("cellular capability should be on")
	}
}

func TestParseModemNoModule(t *testing.T) {
	m := &Meta{Init: map[string]bool{"qmodem": true}}
	m.parseModem(`{"error":"explicit valid modem section required"}`,
		`{"interfaces":{"wan":{"internet":true}},"cellular":{}}`, "", `{"interfaces":[{"id":"wan"}]}`)
	if m.Modem.Present {
		t.Fatalf("should not report a modem: %+v", m.Modem)
	}
	if !m.Modem.Installed || !m.Caps.Cellular {
		t.Fatalf("qmodem installed should keep capability on: %+v", m.Modem)
	}
	if m.Modem.RSRP != 0 || m.Modem.SINR != 0 || m.Modem.Operator != "" {
		t.Fatalf("must not invent signal values: %+v", m.Modem)
	}
}
