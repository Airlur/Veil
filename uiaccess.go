// uiaccess.go — UIAccess 诊断：进程是否持有 UIAccess 令牌、窗口所在的 Z 序层级（band）。
//
// 带 UIAccess 令牌的进程创建的置顶窗口会被系统放进更高的 band，
// 从层级上压过右键菜单与输入法候选窗（同 band 内才需要靠定时器抢顶）。
// 这里只做读取与日志，不改变任何窗口行为。
package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGetWindowBand = user32.NewProc("GetWindowBand")

// Z 序 band（未公开枚举，数值来自公开逆向资料）
const (
	ZBID_DESKTOP  = 1
	ZBID_UIACCESS = 2
)

// hasUIAccess 返回当前进程令牌是否带 UIAccess。
func hasUIAccess() bool {
	var v, n uint32
	err := windows.GetTokenInformation(windows.GetCurrentProcessToken(), windows.TokenUIAccess,
		(*byte)(unsafe.Pointer(&v)), uint32(unsafe.Sizeof(v)), &n)
	return err == nil && v != 0
}

// windowBand 返回窗口所在 band；API 不存在或调用失败时返回 0。
func windowBand(hwnd windows.Handle) uint32 {
	if procGetWindowBand.Find() != nil {
		return 0
	}
	var band uint32
	r, _, _ := procGetWindowBand.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&band)))
	if r == 0 {
		return 0
	}
	return band
}
