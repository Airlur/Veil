// cursor.go — 遮罩期间隐藏物理屏鼠标光标，并给远控端补一个"替身光标"。
//
// 光标由系统画在所有窗口之上，遮罩盖不住，只能把光标本身换掉：
//  1. SetSystemCursor 把系统标准光标全部替换成全透明 → 物理屏看不到光标。
//  2. （可选，默认关闭）远控软件若采集被控端的光标形状，也会拿到透明光标。此时可另建一个小窗口画出原光标：
//     它**不设**捕获排除、Z 序压在遮罩正下方 → 物理屏被遮罩挡住，远控画面里可见。
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

const cursorClassName = "VeilCursorWindow"

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

// cursorShape 是一个光标预渲染好的预乘 ARGB 位图（供 UpdateLayeredWindow 使用）。
type cursorShape struct {
	dc, bmp    uintptr
	pix        []byte // BGRA 预乘，指向 bmp 的像素内存
	w, h       int32
	hotX, hotY int32
}

var (
	cursorsHidden   bool
	cursorShapes    = map[uintptr]*cursorShape{} // 光标句柄 → 形状
	cursorHwnd      windows.Handle
	cursorClassDone bool
	watchdogStarted bool

	// 替身光标当前状态，避免每个 tick 重复提交
	curShown  bool
	curHandle uintptr
	curPos    POINT
)

// restoreSystemCursors 让系统按注册表重新加载全部光标。任何时候调用都安全。
func restoreSystemCursors() {
	procSystemParametersInfoW.Call(SPI_SETCURSORS, 0, 0, 0)
}

// hideCursors 隐藏物理屏光标并启动替身光标。必须在 UI 线程调用。
func hideCursors() {
	c := getConfig()
	if cursorsHidden || !c.HideCursor {
		return
	}
	startCursorWatchdog()

	mapped := 0
	for _, id := range systemCursorIDs {
		before, _, _ := procLoadCursorW.Call(0, id)
		if before == 0 {
			continue
		}
		var shape *cursorShape
		if c.RemoteCursor {
			shape = renderCursorShape(before)
		}
		blank := createBlankCursor()
		if blank == 0 {
			continue
		}
		// SetSystemCursor 会接管并销毁传入的光标，每个 ID 都要新建一个
		if r, _, _ := procSetSystemCursor.Call(blank, id); r == 0 {
			procDestroyCursor.Call(blank)
			continue
		}
		if shape != nil {
			// 替换前后的句柄都登记：GetCursorInfo 返回哪个都能对上
			cursorShapes[before] = shape
			if after, _, _ := procLoadCursorW.Call(0, id); after != 0 {
				cursorShapes[after] = shape
			}
			mapped++
		}
	}
	cursorsHidden = true
	arrow, _, _ := procLoadCursorW.Call(0, 32512)
	if s := cursorShapes[arrow]; s != nil {
		logf("INFO: 物理屏光标已隐藏（替身光标形状 %d 个，箭头 %dx%d）", mapped, s.w, s.h)
	} else {
		logf("INFO: 物理屏光标已隐藏（无替身光标，remoteCursor=%v）", c.RemoteCursor)
	}

	if c.RemoteCursor && createCursorWindow() {
		procSetTimer.Call(uintptr(controlHwnd), TIMER_CURSOR, 10, 0)
		syncCursorWindow()
	}
}

// showCursors 恢复系统光标并销毁替身光标。必须在 UI 线程调用。
func showCursors() {
	if !cursorsHidden {
		return
	}
	procKillTimer.Call(uintptr(controlHwnd), TIMER_CURSOR)
	restoreSystemCursors()
	if cursorHwnd != 0 {
		procDestroyWindow.Call(uintptr(cursorHwnd))
		cursorHwnd = 0
	}
	freed := map[*cursorShape]bool{}
	for _, s := range cursorShapes {
		if !freed[s] {
			freed[s] = true
			procDeleteDC.Call(s.dc)
			procDeleteObject.Call(s.bmp)
		}
	}
	cursorShapes = map[uintptr]*cursorShape{}
	curShown, curHandle = false, 0
	cursorsHidden = false
	logf("INFO: 物理屏光标已恢复")
}

// setRemoteCursor 开关远控端替身光标并立即生效。必须在 UI 线程调用。
func setRemoteCursor(on bool) {
	updateConfig(func(c *Config) {
		c.RemoteCursor = on
		c.RemoteCursorOpt = &on
	})
	logf("INFO: 远控端替身光标 = %v", on)
	if cursorsHidden {
		showCursors()
		hideCursors()
	}
}

// createBlankCursor 生成 32x32 全透明光标（AND 掩码全 1 = 保留屏幕，XOR 掩码全 0 = 不改动）。
func createBlankCursor() uintptr {
	and := make([]byte, 32*32/8)
	xor := make([]byte, 32*32/8)
	for i := range and {
		and[i] = 0xFF
	}
	h, _, _ := procCreateCursor.Call(uintptr(getModuleHandle()), 0, 0, 32, 32,
		uintptr(unsafe.Pointer(&and[0])), uintptr(unsafe.Pointer(&xor[0])))
	return h
}

// renderCursorShape 把光标预渲染成预乘 ARGB 位图。
// 分别画在黑底和白底上反推 alpha：对带 alpha 通道的光标和老式掩码光标都成立，
// 不依赖 DrawIconEx 是否写目标 alpha 通道。
func renderCursorShape(hcur uintptr) *cursorShape {
	var ii ICONINFO
	if r, _, _ := procGetIconInfo.Call(hcur, uintptr(unsafe.Pointer(&ii))); r == 0 {
		return nil
	}
	defer func() {
		if ii.HbmMask != 0 {
			procDeleteObject.Call(uintptr(ii.HbmMask))
		}
		if ii.HbmColor != 0 {
			procDeleteObject.Call(uintptr(ii.HbmColor))
		}
	}()

	var bm BITMAP
	src := ii.HbmColor
	if src == 0 {
		src = ii.HbmMask
	}
	procGetObjectW.Call(uintptr(src), unsafe.Sizeof(bm), uintptr(unsafe.Pointer(&bm)))
	w, h := bm.BmWidth, bm.BmHeight
	if ii.HbmColor == 0 {
		h /= 2 // 单色光标：掩码位图上下两半分别是 AND / XOR
	}
	if w <= 0 || h <= 0 || w > 512 || h > 512 {
		return nil
	}

	onBlack, dcB, bmpB := newDIB(w, h)
	onWhite, dcW, bmpW := newDIB(w, h)
	if dcB == 0 || dcW == 0 {
		return nil
	}
	defer func() {
		procDeleteDC.Call(dcW)
		procDeleteObject.Call(bmpW)
	}()
	for i := range onWhite {
		onWhite[i] = 0xFF
	}
	procDrawIconEx.Call(dcB, 0, 0, hcur, uintptr(w), uintptr(h), 0, 0, DI_NORMAL)
	procDrawIconEx.Call(dcW, 0, 0, hcur, uintptr(w), uintptr(h), 0, 0, DI_NORMAL)

	// 像素为 BGRA。黑底结果 = 预乘色；白底 - 黑底 = 255*(1-alpha)
	for i := 0; i < len(onBlack); i += 4 {
		diff := int(onWhite[i+1]) - int(onBlack[i+1])
		switch {
		case diff < 0:
			// 反色像素（黑底变白、白底变黑）：分层窗口做不了反色，用不透明深灰代替
			onBlack[i], onBlack[i+1], onBlack[i+2], onBlack[i+3] = 0x30, 0x30, 0x30, 0xFF
		default:
			onBlack[i+3] = byte(255 - diff)
		}
	}
	return &cursorShape{dc: dcB, bmp: bmpB, pix: onBlack, w: w, h: h, hotX: int32(ii.XHotspot), hotY: int32(ii.YHotspot)}
}

// newDIB 创建自上而下的 32 位 DIB 并选入内存 DC，像素初始为全 0。
func newDIB(w, h int32) (pix []byte, dc, bmp uintptr) {
	var bi BITMAPINFO
	bi.BmiHeader.BiSize = uint32(unsafe.Sizeof(BITMAPINFOHEADER{}))
	bi.BmiHeader.BiWidth = w
	bi.BmiHeader.BiHeight = -h
	bi.BmiHeader.BiPlanes = 1
	bi.BmiHeader.BiBitCount = 32
	bi.BmiHeader.BiCompression = BI_RGB

	var bits unsafe.Pointer
	bmp, _, _ = procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bi)), DIB_RGB_COLORS, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == nil {
		return nil, 0, 0
	}
	dc, _, _ = procCreateCompatibleDC.Call(0)
	if dc == 0 {
		procDeleteObject.Call(bmp)
		return nil, 0, 0
	}
	procSelectObject.Call(dc, bmp)
	return unsafe.Slice((*byte)(bits), int(w)*int(h)*4), dc, bmp
}

// createCursorWindow 创建替身光标窗口：穿透、不抢焦点、**不设捕获排除**。
// 这里用 UpdateLayeredWindow（逐像素 alpha）没问题——"不能用真分层渲染"的限制只针对遮罩窗口。
func createCursorWindow() bool {
	if !cursorClassDone {
		wc := WNDCLASSEXW{
			CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
			LpfnWndProc:   procDefWindowProcW.Addr(),
			HInstance:     getModuleHandle(),
			LpszClassName: utf16Ptr(cursorClassName),
		}
		if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
			logf("ERROR: 注册替身光标窗口类失败: %v", err)
			return false
		}
		cursorClassDone = true
	}
	hwnd, _, err := procCreateWindowExW.Call(
		uintptr(WS_EX_TOPMOST|WS_EX_TOOLWINDOW|WS_EX_NOACTIVATE|WS_EX_TRANSPARENT|WS_EX_LAYERED),
		uintptr(unsafe.Pointer(utf16Ptr(cursorClassName))),
		uintptr(unsafe.Pointer(utf16Ptr("VeilCursor"))),
		uintptr(WS_POPUP),
		0, 0, 1, 1,
		0, 0, uintptr(getModuleHandle()), 0,
	)
	if hwnd == 0 {
		logf("ERROR: 创建替身光标窗口失败: %v", err)
		return false
	}
	cursorHwnd = windows.Handle(hwnd)
	return true
}

// syncCursorWindow 由 TIMER_CURSOR 驱动：让替身光标跟随真实光标的位置与形状。
func syncCursorWindow() {
	if cursorHwnd == 0 {
		return
	}
	var ci CURSORINFO
	ci.CbSize = uint32(unsafe.Sizeof(ci))
	r, _, _ := procGetCursorInfo.Call(uintptr(unsafe.Pointer(&ci)))
	shape := cursorShapes[uintptr(ci.HCursor)]
	// 光标被应用隐藏（如全屏视频），或是应用自绘光标（系统照常显示它，不需要替身）
	if r == 0 || ci.Flags&CURSOR_SHOWING == 0 || shape == nil {
		if curShown {
			procShowWindow.Call(uintptr(cursorHwnd), SW_HIDE)
			curShown = false
		}
		return
	}
	if len(activeOverlays) == 0 {
		return // 没有遮罩可躲：绝不在物理屏上露出替身
	}
	if curShown && curHandle == uintptr(ci.HCursor) && curPos == ci.PtScreenPos {
		return
	}

	pos := POINT{ci.PtScreenPos.X - shape.hotX, ci.PtScreenPos.Y - shape.hotY}
	size := SIZE{shape.w, shape.h}
	var srcPt POINT
	blend := BLENDFUNCTION{BlendOp: AC_SRC_OVER, SourceConstantAlpha: 255, AlphaFormat: AC_SRC_ALPHA}
	procUpdateLayeredWindow.Call(uintptr(cursorHwnd), 0,
		uintptr(unsafe.Pointer(&pos)), uintptr(unsafe.Pointer(&size)),
		shape.dc, uintptr(unsafe.Pointer(&srcPt)), 0,
		uintptr(unsafe.Pointer(&blend)), ULW_ALPHA)
	if !curShown {
		// 显示的同时直接插到遮罩下方，不经过"最顶层"
		placeCursorWindow(SWP_SHOWWINDOW)
		curShown = true
	}
	curHandle, curPos = uintptr(ci.HCursor), ci.PtScreenPos
}

// placeCursorWindow 把替身光标插到遮罩**正下方**（hWndInsertAfter = 最底下那块遮罩）。
// 绝不能先提到 HWND_TOPMOST 再让遮罩盖回来：中间那一帧替身会露在物理屏上，
// 表现为光标按抢顶周期不停闪烁（已踩坑）。
func placeCursorWindow(extraFlags uintptr) {
	if cursorHwnd == 0 || len(activeOverlays) == 0 {
		return
	}
	// keepOnTop 按顺序提升遮罩，activeOverlays[0] 最先被提、处在遮罩组的最下面
	procSetWindowPos.Call(uintptr(cursorHwnd), uintptr(activeOverlays[0].hwnd), 0, 0, 0, 0,
		uintptr(SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE)|extraFlags)
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
