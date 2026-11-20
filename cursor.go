// cursor.go — 遮罩期间隐藏物理屏鼠标光标。
//
// 光标由系统画在所有窗口之上，遮罩盖不住，只能把光标本身换掉：
// SetSystemCursor 把系统标准光标全部替换成 1x1 的全透明光标。
//
// 为什么是 1x1（针对 UU 远程的实测与分析，改动前先读 AGENTS.md「光标」一节）：
// UU 被控端在"光标原始尺寸小于控制端需要的尺寸"时，不读系统当前光标，
// 而是按注册表 Control Panel\Cursors 直接加载光标文件——拿到的是真实形状。
// 把透明光标做得尽量小，让 UU 始终走这条路径：物理屏不可见，远控端光标正常。
// 32x32 的透明光标会被系统按 DPI 放大到与真实光标同尺寸，UU 就会读到透明形状，
// 表现为远控端光标时有时无。
//
// 恢复机制（光标丢失比遮罩故障严重得多，三重保险）：
//   - 关闭遮罩 / 正常退出：SPI_SETCURSORS 让系统按注册表重载光标
//   - 进程崩溃或被强杀：看门狗进程（Veil.exe -cursor-watchdog <pid>）等主进程结束后恢复
//   - 每次启动：无条件恢复一次
//
// 已知边界：SetSystemCursor 只影响系统标准光标，应用自绘的光标（部分游戏/设计软件）仍会显示。
package main

import (
	"fmt"
	"os"
	"strconv"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 需要替换的系统光标（OCR_*）
var systemCursorIDs = []uintptr{
	32512, // NORMAL
	32513, // IBEAM
	32514, // WAIT
	32515, // CROSS
	32516, // UP
	32631, // NWPEN
	32642, // SIZENWSE
	32643, // SIZENESW
	32644, // SIZEWE
	32645, // SIZENS
	32646, // SIZEALL
	32648, // NO
	32649, // HAND
	32650, // APPSTARTING
	32651, // HELP
	32671, // PIN
	32672, // PERSON
}

var (
	cursorsHidden   bool
	watchdogStarted bool
)

// restoreSystemCursors 让系统按注册表重新加载全部光标。任何时候调用都安全。
func restoreSystemCursors() {
	procSystemParametersInfoW.Call(SPI_SETCURSORS, 0, 0, 0)
}

// hideCursors 隐藏物理屏光标。必须在 UI 线程调用。
func hideCursors() {
	if cursorsHidden || !getConfig().HideCursor {
		return
	}
	startCursorWatchdog()

	replaced := 0
	for _, id := range systemCursorIDs {
		blank := createBlankCursor()
		if blank == 0 {
			continue
		}
		// SetSystemCursor 会接管并销毁传入的光标，每个 ID 都要新建一个
		if r, _, _ := procSetSystemCursor.Call(blank, id); r == 0 {
			procDestroyCursor.Call(blank)
			continue
		}
		replaced++
	}
	cursorsHidden = true
	logf("INFO: 物理屏光标已隐藏（替换 %d/%d 个系统光标）", replaced, len(systemCursorIDs))
}

// showCursors 恢复系统光标。必须在 UI 线程调用。
func showCursors() {
	if !cursorsHidden {
		return
	}
	restoreSystemCursors()
	cursorsHidden = false
	logf("INFO: 物理屏光标已恢复")
}

// createBlankCursor 生成 1x1 全透明光标（AND 掩码全 1 = 保留屏幕，XOR 掩码全 0 = 不改动）。
// 尺寸必须保持最小，原因见文件头。
func createBlankCursor() uintptr {
	and := []byte{0xFF, 0xFF, 0xFF, 0xFF}
	xor := []byte{0, 0, 0, 0}
	h, _, _ := procCreateCursor.Call(uintptr(getModuleHandle()), 0, 0, 1, 1,
		uintptr(unsafe.Pointer(&and[0])), uintptr(unsafe.Pointer(&xor[0])))
	return h
}

// ---------- 看门狗 ----------

const watchdogFlag = "-cursor-watchdog"

// startCursorWatchdog 拉起看门狗进程（整个进程生命周期只需一个）。
// 用 ShellExecute 而不是 CreateProcess：UIAccess 版的 exe 不能被 CreateProcess 直接启动。
func startCursorWatchdog() {
	if watchdogStarted {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		logf("WARNING: 看门狗未启动（取不到 exe 路径）: %v", err)
		return
	}
	args := fmt.Sprintf("%s %d", watchdogFlag, os.Getpid())
	if err := windows.ShellExecute(0, utf16Ptr("open"), utf16Ptr(exe), utf16Ptr(args), nil, windows.SW_HIDE); err != nil {
		logf("WARNING: 看门狗启动失败（崩溃后需重启 Veil 才能恢复光标）: %v", err)
		return
	}
	watchdogStarted = true
	logf("INFO: 光标看门狗已启动")
}

// runCursorWatchdog 是看门狗进程的全部逻辑：等主进程结束，然后恢复光标。
// 不创建窗口、不占单实例互斥体。
func runCursorWatchdog(pidArg string) {
	pid, err := strconv.ParseUint(pidArg, 10, 32)
	if err != nil {
		return
	}
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		// 主进程已经不在了：直接恢复
		restoreSystemCursors()
		return
	}
	defer windows.CloseHandle(h)
	windows.WaitForSingleObject(h, windows.INFINITE)
	restoreSystemCursors()
}
