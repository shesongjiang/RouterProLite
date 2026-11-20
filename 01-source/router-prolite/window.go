package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// windowState 是窗口尺寸/位置/最大化状态的持久化结构。
type windowState struct {
	X      int  `json:"x"`
	Y      int  `json:"y"`
	W      int  `json:"w"`
	H      int  `json:"h"`
	Max    bool `json:"max"`
	HasPos bool `json:"hasPos"`
}

func windowStateFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "ProLite", "window.json")
}

func loadWindowState() windowState {
	st := windowState{W: 1120, H: 760}
	if b, err := os.ReadFile(windowStateFile()); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	if st.W <= 0 || st.H <= 0 {
		st.W, st.H = 1120, 760
	}
	if st.W < 760 {
		st.W = 760
	}
	if st.H < 520 {
		st.H = 520
	}
	return st
}

func saveWindowState(st windowState) {
	_ = os.MkdirAll(filepath.Dir(windowStateFile()), 0o700)
	if b, err := json.Marshal(st); err == nil {
		_ = os.WriteFile(windowStateFile(), b, 0o600)
	}
}

// beforeClose 在窗口关闭前记录尺寸、位置与最大化状态。
// 优先用窗口放置信息读取还原后的普通尺寸，最大化时也不会丢掉窗口大小。
func (a *App) beforeClose(ctx context.Context) bool {
	if a.ctx == nil {
		return false
	}
	if st, ok := hwndWindowState(); ok {
		saveWindowState(st)
		return false
	}
	w, h := runtime.WindowGetSize(a.ctx)
	x, y := runtime.WindowGetPosition(a.ctx)
	saveWindowState(windowState{X: x, Y: y, W: w, H: h, Max: runtime.WindowIsMaximised(a.ctx), HasPos: true})
	return false
}

func (a *App) WindowMinimise() {
	runtime.WindowMinimise(a.ctx)
}

func (a *App) WindowToggleMaximise() {
	runtime.WindowToggleMaximise(a.ctx)
}

func (a *App) WindowIsMaximised() bool {
	return runtime.WindowIsMaximised(a.ctx)
}
