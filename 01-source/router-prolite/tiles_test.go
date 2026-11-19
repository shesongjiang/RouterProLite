package main

import "testing"

func TestBuildTilesKeepsCoreTiles(t *testing.T) {
	snap := &Snapshot{
		WAN:     &WANInfo{Up: true, Proto: "dhcp", Device: "wan", Gateway: "192.168.1.1"},
		Clients: []Client{{IP: "192.168.1.10", Online: true}},
	}
	meta := &Meta{
		Caps:     Capabilities{Wireless: true, Cellular: true, SQM: true, AdGuard: true, Proxy: true, Mwan3: true},
		Wireless: WirelessInfo{Present: true, Enabled: true, SSID: "test", Channel: "6"},
		Modem:    CellularInfo{Present: true, Internet: true, Count: 1},
		Init:     map[string]bool{"adguardhome": true, "passwall": true},
		Running:  map[string]bool{"sqm": true, "adguardhome": true, "passwall": true, "dnsmasq": true},
		Mwan:     MWANInfo{Installed: true, Running: true},
	}
	tiles := buildTiles(snap, meta)
	if len(tiles) > 8 {
		t.Fatalf("tile count %d exceeds cap", len(tiles))
	}
	keys := map[string]bool{}
	for _, tile := range tiles {
		keys[tile.Key] = true
	}
	for _, want := range []string{"wan", "firewall", "dhcp"} {
		if !keys[want] {
			t.Fatalf("core tile %q dropped: %+v", want, keys)
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
