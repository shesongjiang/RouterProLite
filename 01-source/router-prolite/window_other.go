//go:build !windows

package main

// 非 Windows 平台没有 DWM，保持系统默认窗口形状。
func applyWindowChrome() {}

func hwndWindowState() (windowState, bool) { return windowState{}, false }
