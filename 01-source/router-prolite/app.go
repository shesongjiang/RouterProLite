package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	stdruntime "runtime"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx    context.Context
	router *Router
	store  *configStore

	jobsMu sync.Mutex
	jobs   map[string]*InstallJob

	storageMu sync.Mutex
}

func NewApp() *App {
	return &App{
		router: newRouter(),
		store:  newConfigStore(),
		jobs:   map[string]*InstallJob{},
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	applyWindowChrome()
	if st := loadWindowState(); st.HasPos && !st.Max {
		runtime.WindowSetPosition(ctx, st.X, st.Y)
	}
	cfg := a.store.Load()
	if cfg.Pinned {
		runtime.WindowSetAlwaysOnTop(ctx, true)
	}
}

func (a *App) shutdown(ctx context.Context) {
	a.router.Disconnect()
}

func (a *App) GetConfig() ConfigView {
	cfg := a.store.Load()
	return ConfigView{
		Saved:       cfg.Saved,
		Host:        cfg.Host,
		Port:        cfg.Port,
		User:        cfg.User,
		Auth:        cfg.Auth,
		KeyPath:     cfg.KeyPath,
		Fingerprint: cfg.Fingerprint,
		Pinned:      cfg.Pinned,
		Platform:    stdruntime.GOOS,
		Version:     appVersion,
	}
}

func (a *App) Connect(in ConnectionInput) (*ConnectionInfo, error) {
	cfg := a.store.Load()
	info, err := a.router.Connect(in, cfg.Fingerprint)
	if err != nil {
		return nil, err
	}
	if in.Remember {
		cfg.Host = info.Host
		cfg.Port = info.Port
		cfg.User = info.User
		cfg.Auth = in.Auth
		cfg.KeyPath = in.KeyPath
		cfg.Saved = true
	}
	cfg.Fingerprint = info.Fingerprint
	if err := a.store.Save(cfg); err != nil {
		return info, fmt.Errorf("连接成功，但保存配置失败：%w", err)
	}
	return info, nil
}

func (a *App) Disconnect() {
	a.router.Disconnect()
}

func (a *App) GetSnapshot() Snapshot {
	return a.router.Snapshot()
}

func (a *App) GetLab(refresh bool) (*LabData, error) {
	return a.router.Lab(refresh)
}

func (a *App) GetConsole(force bool) (*ConsoleData, error) {
	return a.router.Console(force)
}

func (a *App) GetLogs(kind string, filter string, lines int) (string, error) {
	return a.router.Logs(kind, filter, lines)
}

func (a *App) GetConfigs() ([]string, error) {
	return a.router.GetConfigs()
}

func (a *App) GetConfigValues(config string) (map[string]map[string]any, error) {
	return a.router.GetConfigValues(config)
}

func (a *App) SetUCIOption(in UCIOptionInput) error {
	return a.router.SetUCIOption(in)
}

func (a *App) SetUCIList(in UCIListInput) error {
	return a.router.SetUCIList(in)
}

func (a *App) DeleteUCIOption(config string, section string, option string) error {
	return a.router.DeleteUCIOption(config, section, option)
}

func (a *App) SetSystem(in SystemInput) error {
	return a.router.SetSystem(in)
}

func (a *App) SetWiFi(in WiFiInput) error {
	return a.router.SetWiFi(in)
}

func (a *App) SetDHCPPool(in DHCPPoolInput) error {
	return a.router.SetDHCPPool(in)
}

func (a *App) SetUPnP(on bool) error {
	return a.router.SetUPnP(on)
}

func (a *App) InterfaceAction(name string, action string) error {
	return a.router.InterfaceAction(name, action)
}

func (a *App) ReloadService(name string) error {
	return a.router.ReloadService(name)
}

func (a *App) SetAlwaysOnTop(on bool) error {
	runtime.WindowSetAlwaysOnTop(a.ctx, on)
	cfg := a.store.Load()
	cfg.Pinned = on
	return a.store.Save(cfg)
}

func (a *App) CloseApp() {
	runtime.Quit(a.ctx)
}

func (a *App) PickKeyFile() (string, error) {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".ssh")
	if _, err := os.Stat(dir); err != nil {
		dir = home
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "选择 SSH 私钥",
		DefaultDirectory: dir,
		ShowHiddenFiles:  true,
		Filters: []runtime.FileFilter{
			{DisplayName: "SSH 私钥", Pattern: "*"},
		},
	})
	if err != nil {
		return "", err
	}
	return path, nil
}

func (a *App) ResetFingerprint() error {
	cfg := a.store.Load()
	cfg.Fingerprint = ""
	return a.store.Save(cfg)
}

func (a *App) CopyText(text string) error {
	return runtime.ClipboardSetText(a.ctx, text)
}

func (a *App) AddPortForward(in PortForwardInput) error {
	return a.router.AddPortForward(in)
}

func (a *App) TogglePortForward(id string, enabled bool) error {
	return a.router.TogglePortForward(id, enabled)
}

func (a *App) DeletePortForward(id string) error {
	return a.router.DeletePortForward(id)
}

func (a *App) SetStaticLease(in StaticLeaseInput) error {
	return a.router.SetStaticLease(in)
}

func (a *App) SetBlocked(mac string, blocked bool) error {
	return a.router.SetBlocked(mac, blocked)
}

func (a *App) SetDNS(servers string) error {
	return a.router.SetDNS(servers)
}

func (a *App) SetFullCone(on bool) error {
	return a.router.SetFullCone(on)
}

func (a *App) ToggleService(name string, on bool) error {
	return a.router.ToggleService(name, on)
}

func (a *App) WanAction(action string) error {
	return a.router.WANAction(action)
}

func (a *App) ReloadNetwork() error {
	return a.router.ReloadNetwork()
}

func (a *App) RebootRouter() error {
	err := a.router.Reboot()
	a.router.Disconnect()
	return err
}

func (a *App) RunProbe(kind string, target string) (ProbeResult, error) {
	target = strings.TrimSpace(target)
	if err := validateTarget(target); err != nil {
		return ProbeResult{}, err
	}
	var cmd string
	var timeout time.Duration
	switch kind {
	case "ping":
		cmd = "ping -c 4 -W 2 " + shq(target)
		timeout = 20 * time.Second
	case "trace":
		cmd = "traceroute -n -m 12 -q 1 -w 2 " + shq(target)
		timeout = 40 * time.Second
	default:
		return ProbeResult{}, errors.New("不支持的探测类型")
	}
	res, err := a.router.RunCommand(cmd, timeout)
	res.Command = cmd
	if err != nil {
		return res, err
	}
	return res, nil
}

func (a *App) ExecCommand(cmd string) (ProbeResult, error) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ProbeResult{}, errors.New("请输入命令")
	}
	if len(cmd) > 2000 {
		return ProbeResult{}, errors.New("命令过长")
	}
	res, err := a.router.RunCommand(cmd, 25*time.Second)
	res.Command = cmd
	if err != nil {
		return res, err
	}
	return res, nil
}
