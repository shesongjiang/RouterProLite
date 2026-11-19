package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DeviceWritePolicy 是真实写入的“设备级授权文件”。
// 默认不存在 -> 所有真实安装都会被拒绝，避免误操作。
// 文件位置：<UserConfigDir>/ProLite/device-write-allow.json
type DeviceWritePolicy struct {
	Host           string `json:"host"`
	AllowOverlay   bool   `json:"allowOverlay"`
	AllowTmp       bool   `json:"allowTmp"`
	AllowDangerous bool   `json:"allowDangerous"`
	Note           string `json:"note"`
}

// DeviceWritePolicyView 供前端展示授权状态，不触发任何写入。
type DeviceWritePolicyView struct {
	Configured     bool   `json:"configured"`
	Path           string `json:"path"`
	Host           string `json:"host"`
	AllowOverlay   bool   `json:"allowOverlay"`
	AllowTmp       bool   `json:"allowTmp"`
	AllowDangerous bool   `json:"allowDangerous"`
	Matches        bool   `json:"matches"`
	Error          string `json:"error"`
}

func deviceWritePolicyPath() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = "."
	}
	return filepath.Join(base, "ProLite", "device-write-allow.json")
}

func loadDeviceWritePolicy() (*DeviceWritePolicy, error) {
	path := deviceWritePolicyPath()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("未配置设备写入授权文件：%s（默认禁止真实安装）", path)
		}
		return nil, fmt.Errorf("读取设备写入授权文件失败：%w", err)
	}
	var p DeviceWritePolicy
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("设备写入授权文件不是合法 JSON：%w", err)
	}
	p.Host = strings.TrimSpace(p.Host)
	if p.Host == "" {
		return nil, fmt.Errorf("授权文件缺少 host，无法绑定目标设备")
	}
	if !p.AllowOverlay || !p.AllowTmp {
		return nil, fmt.Errorf("授权文件必须同时设置 allowOverlay=true 与 allowTmp=true")
	}
	return &p, nil
}

// GetDeviceWritePolicy 返回授权文件状态；未配置时 configured=false，不报错。
func (a *App) GetDeviceWritePolicy() *DeviceWritePolicyView {
	view := &DeviceWritePolicyView{Path: deviceWritePolicyPath()}
	pol, err := loadDeviceWritePolicy()
	if err != nil {
		view.Error = err.Error()
		return view
	}
	view.Configured = true
	view.Host = pol.Host
	view.AllowOverlay = pol.AllowOverlay
	view.AllowTmp = pol.AllowTmp
	view.AllowDangerous = pol.AllowDangerous
	host := a.router.TargetHost()
	view.Matches = host != "" && host == pol.Host
	if !view.Matches {
		view.Error = fmt.Sprintf("授权文件绑定 %s，当前设备 %s", pol.Host, orDash(host))
	}
	return view
}

// authorizeDeviceWrite 在真正下载/写入前做设备级校验：
// 授权文件存在且 host 完全匹配、确认文本等于当前设备地址、用户勾选写入与分区、
// 危险包同时获得授权文件与界面确认。
func (a *App) authorizeDeviceWrite(req InstallRequest, includeDangerous bool) error {
	pol, err := loadDeviceWritePolicy()
	if err != nil {
		return err
	}
	host := a.router.TargetHost()
	if host == "" {
		return errNotConnected
	}
	if pol.Host != host {
		return fmt.Errorf("授权文件绑定的是 %s，当前连接的是 %s；请修改或删除 %s", pol.Host, host, deviceWritePolicyPath())
	}
	if strings.TrimSpace(req.ConfirmText) != host {
		return fmt.Errorf("请在确认框中逐字输入当前设备地址 %s", host)
	}
	if !req.Confirmed || !req.ConfirmWrites {
		return fmt.Errorf("请勾选“我确认写入设备”并确认写入 overlay 与 /tmp")
	}
	if includeDangerous && !pol.AllowDangerous {
		return fmt.Errorf("计划包含危险包，但授权文件未设置 allowDangerous=true")
	}
	if includeDangerous && !req.AllowDangerous {
		return fmt.Errorf("计划包含危险包，请在界面勾选危险包额外确认")
	}
	return nil
}
