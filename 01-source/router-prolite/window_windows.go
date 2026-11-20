//go:build windows

package main

import (
	"math"
	"syscall"
	"time"
	"unsafe"
)

const (
	dwmwaWindowCornerPreference = 33
	dwmwaBorderColor            = 34
	dwmcpRound                  = 2
	dwmColorNone                = 0xFFFFFFFE
)

type winRect struct {
	Left, Top, Right, Bottom int32
}

type osVersionInfoW struct {
	Size       uint32
	Major      uint32
	Minor      uint32
	Build      uint32
	PlatformID uint32
	CSDVersion [128]uint16
}

type winPoint struct{ X, Y int32 }

type windowPlacement struct {
	Length         uint32
	Flags          uint32
	ShowCmd        uint32
	MinPosition    winPoint
	MaxPosition    winPoint
	NormalPosition winRect
}

var (
	user32DLL                 = syscall.NewLazyDLL("user32.dll")
	procFindWindowW           = user32DLL.NewProc("FindWindowW")
	procGetWindowRect         = user32DLL.NewProc("GetWindowRect")
	procIsZoomed              = user32DLL.NewProc("IsZoomed")
	procSetWindowRgn          = user32DLL.NewProc("SetWindowRgn")
	procGetWindowPlacement    = user32DLL.NewProc("GetWindowPlacement")
	procGetDpiForWindow       = user32DLL.NewProc("GetDpiForWindow")
	gdi32DLL                  = syscall.NewLazyDLL("gdi32.dll")
	procCreateRoundRectRgn    = gdi32DLL.NewProc("CreateRoundRectRgn")
	ntdllDLL                  = syscall.NewLazyDLL("ntdll.dll")
	procRtlGetVersion         = ntdllDLL.NewProc("RtlGetVersion")
	dwmapiDLL                 = syscall.NewLazyDLL("dwmapi.dll")
	procDwmSetWindowAttribute = dwmapiDLL.NewProc("DwmSetWindowAttribute")
)

func windowsBuild() uint32 {
	info := osVersionInfoW{Size: uint32(unsafe.Sizeof(osVersionInfoW{}))}
	procRtlGetVersion.Call(uintptr(unsafe.Pointer(&info)))
	return info.Build
}

// hwndWindowState 通过窗口放置信息读取“还原后的尺寸/位置”，
// 最大化时也能拿到普通窗口大小，避免把全屏尺寸当成下次启动的默认值。
func hwndWindowState() (windowState, bool) {
	title, _ := syscall.UTF16PtrFromString("Pro-Lite")
	h, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(title)))
	if h == 0 {
		return windowState{}, false
	}
	maxed, _, _ := procIsZoomed.Call(h)
	var wp windowPlacement
	wp.Length = uint32(unsafe.Sizeof(wp))
	ok, _, _ := procGetWindowPlacement.Call(h, uintptr(unsafe.Pointer(&wp)))
	if ok == 0 {
		return windowState{}, false
	}
	scale := 1.0
	if dpi, _, _ := procGetDpiForWindow.Call(h); dpi > 0 {
		scale = float64(dpi) / 96.0
	}
	r := wp.NormalPosition
	return windowState{
		X:      int(math.Round(float64(r.Left) / scale)),
		Y:      int(math.Round(float64(r.Top) / scale)),
		W:      int(math.Round(float64(r.Right-r.Left) / scale)),
		H:      int(math.Round(float64(r.Bottom-r.Top) / scale)),
		Max:    maxed != 0,
		HasPos: true,
	}, true
}

// roundWindowRegion 用窗口区域裁出圆角；无边框窗口在部分 Win11 版本上
// DWM 圆角不生效，区域裁剪是确定性的兜底，最大化时移除区域避免四角缺块。
func roundWindowRegion(hwnd uintptr) {
	// 等 WebView2 完成首帧再裁剪，过早设置会被窗口初始化流程覆盖。
	time.Sleep(1500 * time.Millisecond)
	var lastW, lastH, appliedW, appliedH int32
	var lastMax, appliedMax bool
	changedAt := time.Now()
	first := true
	for {
		var r winRect
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		w, h := r.Right-r.Left, r.Bottom-r.Top
		maxed, _, _ := procIsZoomed.Call(hwnd)
		isMax := maxed != 0
		if w != lastW || h != lastH || isMax != lastMax {
			lastW, lastH, lastMax = w, h, isMax
			changedAt = time.Now()
		}
		// 尺寸稳定 350ms 后再设置，避免拖动缩放过程中反复触发。
		if (first || time.Since(changedAt) >= 350*time.Millisecond) &&
			(w != appliedW || h != appliedH || isMax != appliedMax) {
			if isMax {
				// 最大化时移除区域，让窗口铺满屏幕，避免四角缺块。
				procSetWindowRgn.Call(hwnd, 0, 1)
			} else if w > 0 && h > 0 {
				// 直径 20px ≈ 10px 圆角，与 Win11 默认圆角观感一致。
				hrgn, _, _ := procCreateRoundRectRgn.Call(0, 0, uintptr(w+1), uintptr(h+1), 20, 20)
				if hrgn != 0 {
					procSetWindowRgn.Call(hwnd, hrgn, 1)
				}
			}
			appliedW, appliedH, appliedMax = w, h, isMax
			first = false
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// applyWindowChrome 让 Windows 11 的 DWM 直接给窗口做圆角；
// Win10 或 DWM 不可用时保持系统方角，不会出现半截圆角。
func applyWindowChrome() {
	go func() {
		var hwnd uintptr
		title, _ := syscall.UTF16PtrFromString("Pro-Lite")
		for i := 0; i < 40; i++ {
			h, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(title)))
			if h != 0 {
				hwnd = h
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if hwnd == 0 {
			return
		}
		pref := int32(dwmcpRound)
		hr, _, _ := procDwmSetWindowAttribute.Call(hwnd, dwmwaWindowCornerPreference,
			uintptr(unsafe.Pointer(&pref)), unsafe.Sizeof(pref))
		if hr == 0 {
			// 去掉系统 1px 描边，避免圆角外侧出现亮线。
			none := uint32(dwmColorNone)
			_, _, _ = procDwmSetWindowAttribute.Call(hwnd, dwmwaBorderColor,
				uintptr(unsafe.Pointer(&none)), unsafe.Sizeof(none))
		}
		if windowsBuild() >= 22000 {
			roundWindowRegion(hwnd)
		}
	}()
}
