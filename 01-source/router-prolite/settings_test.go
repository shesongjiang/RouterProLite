package main

import "testing"

func TestRadioShouldDisable(t *testing.T) {
	cfg := UCIConfig{
		"radio0": {".type": "wifi-device", "disabled": "0"},
		"main":   {".type": "wifi-iface", "device": "radio0", "disabled": "0"},
		"guest":  {".type": "wifi-iface", "device": "radio0", "disabled": "0"},
	}
	if radioShouldDisable(cfg, "radio0", "guest", true) {
		t.Fatal("disabling one SSID must not disable the radio")
	}
	if radioShouldDisable(cfg, "radio0", "guest", false) {
		t.Fatal("enabled SSID must keep the radio enabled")
	}

	cfg["main"]["disabled"] = "1"
	if !radioShouldDisable(cfg, "radio0", "guest", true) {
		t.Fatal("radio should be disabled when every SSID is disabled")
	}
	if radioShouldDisable(cfg, "radio0", "guest", false) {
		t.Fatal("enabling one SSID should enable the radio")
	}

	single := UCIConfig{
		"radio1": {".type": "wifi-device"},
		"wifi0":  {".type": "wifi-iface", "device": "radio1", "disabled": "0"},
	}
	if !radioShouldDisable(single, "radio1", "wifi0", true) {
		t.Fatal("single SSID should follow the iface state")
	}
	if !radioShouldDisable(UCIConfig{}, "radio2", "missing", true) {
		t.Fatal("unknown radio should follow the iface state")
	}
	if radioShouldDisable(UCIConfig{}, "", "missing", false) {
		t.Fatal("empty radio should follow the iface state")
	}
}
