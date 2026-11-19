// monitor.go — 显示器枚举与 DPI 感知。
// 全部使用物理像素：200% 缩放下若用逻辑像素，遮罩只会盖住屏幕的四分之一。
package main

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Monitor 描述一块物理显示器。
type Monitor struct {
	Handle  windows.Handle
	Rect    RECT // 物理像素坐标（rcMonitor，含任务栏区域）
	Device  string
	Primary bool
}

// monEnumCallback 必须存包级变量，否则被 GC 回收会导致进程随机崩溃。
var monEnumCallback uintptr

// setProcessDpiAwareness 声明 Per-Monitor V2，使坐标全部按物理像素计算。
func setProcessDpiAwareness() {
	r, _, _ := procSetProcessDpiAwarenessContext.Call(uintptr(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2))
	if r == 0 {
		// 老系统或已被设置过：退回 Per-Monitor V1
		shcore := windows.NewLazySystemDLL("shcore.dll")
		proc := shcore.NewProc("SetProcessDpiAwareness")
		proc.Call(2) // PROCESS_PER_MONITOR_DPI_AWARE
	}
}

// enumMonitors 枚举所有显示器。
func enumMonitors() []Monitor {
	var out []Monitor

	cb := syscall.NewCallback(func(hMonitor uintptr, hdc uintptr, lprc uintptr, data uintptr) uintptr {
		var mi MONITORINFOEXW
		mi.CbSize = uint32(unsafe.Sizeof(mi))
		r, _, _ := procGetMonitorInfoW.Call(hMonitor, uintptr(unsafe.Pointer(&mi)))
		if r == 0 {
			return 1 // continue enumeration
		}
		out = append(out, Monitor{
			Handle:  windows.Handle(hMonitor),
			Rect:    mi.RcMonitor,
			Device:  utf16ToString(mi.SzDevice[:]),
			Primary: mi.DwFlags&MONITORINFOF_PRIMARY != 0,
		})
		return 1
	})
	monEnumCallback = cb

	procEnumDisplayMonitors.Call(0, 0, cb, 0)
	return out
}
