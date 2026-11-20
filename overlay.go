// overlay.go — 遮罩窗口：全屏、置顶、鼠标穿透、对系统采集"隐身"。
//
// 核心机制：SetWindowDisplayAffinity(hwnd, WDA_EXCLUDEFROMCAPTURE)
// 让窗口只出现在物理显示器上，而在 DXGI 桌面复制 / WGC / BitBlt 等所有系统采集路径中被剔除。
// 外部远控软件（UU 等）采集到的是遮罩**背后**的真实桌面。
//
// 三条硬性要求，缺一不可：
//  1. 捕获排除（WDA_EXCLUDEFROMCAPTURE）
//  2. 鼠标穿透（WM_NCHITTEST 返回 HTTRANSPARENT）
//  3. 不抢焦点（WS_EX_NOACTIVATE + SW_SHOWNOACTIVATE）
package main

import (
	"image"
	"image/color"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const overlayClassName = "VeilOverlayWindow"
const controlClassName = "VeilControlWindow"

var (
	overlayWndProcCallback uintptr
	controlWndProcCallback uintptr

	// 当前活动的遮罩窗口
	activeOverlays []*overlayWindow
	// 控制窗口句柄（跨线程命令入口）
	controlHwnd windows.Handle
	// 遮罩是否处于开启状态
	overlayOn bool
	// 窗口类是否已注册
	overlayClassRegistered bool
)

type overlayWindow struct {
	hwnd windows.Handle
	mon  Monitor
	dc   windows.Handle // 内存 DC
	bmp  windows.Handle // DIB 位图
}

// ---------- 创建与销毁 ----------

func registerOverlayClass() error {
	if overlayClassRegistered {
		return nil
	}
	hInst := getModuleHandle()

	wc := WNDCLASSEXW{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		Style:         0,
		LpfnWndProc:   overlayWndProcCallback,
		HInstance:     hInst,
		HCursor:       loadCursor(IDC_ARROW),
		HbrBackground: 0, // 背景由 WM_PAINT 全权负责，避免闪烁
		LpszClassName: utf16Ptr(overlayClassName),
	}
	r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return err
	}
	overlayClassRegistered = true
	return nil
}

func loadCursor(id uintptr) windows.Handle {
	h, _, _ := procLoadCursorW.Call(0, id)
	return windows.Handle(h)
}

// createOneOverlay 为一块显示器创建遮罩窗口。
func createOneOverlay(mon Monitor) (*overlayWindow, error) {
	w := mon.Rect.Right - mon.Rect.Left
	h := mon.Rect.Bottom - mon.Rect.Top
	if w <= 0 || h <= 0 {
		return nil, syscall.EINVAL
	}

	// 点击穿透必须 WS_EX_LAYERED | WS_EX_TRANSPARENT 一起：
	// 单独的 WS_EX_TRANSPARENT 只管绘制顺序，对普通窗口的鼠标命中测试不可靠。
	// 注意：只声明 LAYERED 样式、但绝不用 UpdateLayeredWindow/SetLayeredWindowAttributes，
	// 窗口仍是普通 GDI 渲染，这样 WDA_EXCLUDEFROMCAPTURE 才可能继续生效。
	exStyle := uintptr(WS_EX_TOPMOST | WS_EX_TOOLWINDOW | WS_EX_NOACTIVATE | WS_EX_TRANSPARENT | WS_EX_LAYERED)
	style := uintptr(WS_POPUP)

	hwnd, _, err := procCreateWindowExW.Call(
		exStyle,
		uintptr(unsafe.Pointer(utf16Ptr(overlayClassName))),
		uintptr(unsafe.Pointer(utf16Ptr("Veil"))),
		style,
		uintptr(mon.Rect.Left), uintptr(mon.Rect.Top),
		uintptr(w), uintptr(h),
		0, 0, uintptr(getModuleHandle()), 0,
	)
	if hwnd == 0 {
		return nil, err
	}

	ow := &overlayWindow{hwnd: windows.Handle(hwnd), mon: mon}

	// 绘制内容按该显示器的物理像素尺寸生成（cover 裁切，无变形）
	img, _ := currentBackground(int(w), int(h))
	if err := ow.loadBitmap(img); err != nil {
		procDestroyWindow.Call(hwnd)
		return nil, err
	}

	// 关键：标记为"不被捕获"。必须在窗口有效后调用。
	if setExcludeFromCapture(ow.hwnd) {
		logf("INFO: 遮罩已创建 %s (%dx%d) affinity=OK", mon.Device, w, h)
	} else {
		logf("ERROR: %s SetWindowDisplayAffinity 失败，远控端仍会看到遮罩", mon.Device)
	}

	// SW_SHOWNOACTIVATE：显示但不抢焦点，否则会打断用户正在输入的内容
	// 注意：WS_EX_LAYERED 窗口必须先激活一次分层属性，否则整个窗口根本不渲染
	// （表现为"开启遮罩后屏幕没有任何变化"）——alpha 255 = 完全不透明，视觉无变化
	procSetLayeredWindowAttributes.Call(hwnd, 0, 255, uintptr(LWA_ALPHA))
	procShowWindow.Call(hwnd, SW_SHOWNOACTIVATE)
	// 显示后再设一次，防止某些驱动在首次 ShowWindow 时重置状态
	setExcludeFromCapture(ow.hwnd)
	// band=2 (ZBID_UIACCESS) 才能从层级上压住右键菜单/输入法候选窗；band=1 是普通桌面层
	logf("INFO: 遮罩层级 %s band=%d uiAccess=%v", mon.Device, windowBand(ow.hwnd), hasUIAccess())

	return ow, nil
}

// setExcludeFromCapture 返回是否成功；失败意味着系统不支持（Win10 2004 以下）。
func setExcludeFromCapture(hwnd windows.Handle) bool {
	r, _, _ := procSetWindowDisplayAffinity.Call(uintptr(hwnd), uintptr(WDA_EXCLUDEFROMCAPTURE))
	return r != 0
}

// loadBitmap 把图像写入 DIB 并选入内存 DC。
func (ow *overlayWindow) loadBitmap(img *image.RGBA) error {
	ow.releaseBitmap()

	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	screenDC, _, _ := procGetDC.Call(0)
	if screenDC == 0 {
		return syscall.EINVAL
	}
	defer procReleaseDC.Call(0, screenDC)

	var bi BITMAPINFO
	bi.BmiHeader.BiSize = uint32(unsafe.Sizeof(BITMAPINFOHEADER{}))
	bi.BmiHeader.BiWidth = int32(w)
	bi.BmiHeader.BiHeight = -int32(h) // 负值表示自上而下
	bi.BmiHeader.BiPlanes = 1
	bi.BmiHeader.BiBitCount = 32
	bi.BmiHeader.BiCompression = BI_RGB

	var bits unsafe.Pointer
	hbmp, _, err := procCreateDIBSection.Call(
		screenDC,
		uintptr(unsafe.Pointer(&bi)),
		DIB_RGB_COLORS,
		uintptr(unsafe.Pointer(&bits)),
		0, 0,
	)
	if hbmp == 0 || bits == nil {
		return err
	}

	memDC, _, err := procCreateCompatibleDC.Call(screenDC)
	if memDC == 0 {
		procDeleteObject.Call(hbmp)
		return err
	}
	procSelectObject.Call(memDC, hbmp)

	// RGBA -> BGRA，一次性批量转换
	dst := unsafe.Slice((*byte)(bits), w*h*4)
	src := img.Pix
	stride := img.Stride
	n := 0
	for y := 0; y < h; y++ {
		row := src[y*stride:]
		for x := 0; x < w; x++ {
			dst[n+0] = row[x*4+2] // B
			dst[n+1] = row[x*4+1] // G
			dst[n+2] = row[x*4+0] // R
			dst[n+3] = 255
			n += 4
		}
	}

	ow.bmp = windows.Handle(hbmp)
	ow.dc = windows.Handle(memDC)
	return nil
}

func (ow *overlayWindow) releaseBitmap() {
	if ow.bmp != 0 {
		procDeleteObject.Call(uintptr(ow.bmp))
		ow.bmp = 0
	}
	if ow.dc != 0 {
		procDeleteDC.Call(uintptr(ow.dc))
		ow.dc = 0
	}
}

func (ow *overlayWindow) paint(hdc uintptr) {
	if ow.dc == 0 {
		// 没有位图时涂黑兜底
		fillBlack(hdc)
		return
	}
	var rc RECT
	procGetClientRect.Call(uintptr(ow.hwnd), uintptr(unsafe.Pointer(&rc)))
	if rc.Right > 0 && rc.Bottom > 0 {
		procBitBlt.Call(hdc, 0, 0, uintptr(rc.Right), uintptr(rc.Bottom), uintptr(ow.dc), 0, 0, SRCCOPY)
	}
}

func fillBlack(hdc uintptr) {
	brush, _, _ := procCreateSolidBrush.Call(0x00000000)
	if brush == 0 {
		return
	}
	defer procDeleteObject.Call(brush)
	var rc RECT
	rc.Left, rc.Top, rc.Right, rc.Bottom = 0, 0, 10000, 10000
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), brush)
}

// ---------- 开启 / 关闭 ----------

// uiThreadID 是 UI 线程的线程 ID（uiThread 启动时记录）。
// DestroyWindow 只能销毁本线程创建的窗口，跨线程调用会静默失败——
// 所以所有遮罩操作都必须在这个线程执行。
var uiThreadID uint32

// previewWasOn 记录预览前遮罩是否已开启（决定 2.5 秒后恢复还是关闭）
var previewWasOn bool

func inUIThread() bool {
	tid, _, _ := procGetCurrentThreadId.Call()
	return uint32(tid) == uiThreadID
}

// enableOverlay 在所有（或主）显示器上铺遮罩。
// 自动线程纠正：非 UI 线程调用时转发到 UI 线程，避免跨线程创建窗口。
func enableOverlay() {
	if !inUIThread() {
		logf("WARNING: enableOverlay 被非 UI 线程调用(当前=%d, UI=%d)，已转发", curThreadID(), uiThreadID)
		postCmd(cmdEnable)
		return
	}
	if overlayOn {
		return
	}
	if err := registerOverlayClass(); err != nil {
		logf("ERROR: 注册窗口类失败: %v", err)
		return
	}

	mons := enumMonitors()
	if len(mons) == 0 {
		logf("ERROR: 未枚举到任何显示器")
		return
	}

	c := getConfig()
	// 老配置/首次使用：没有生效记录时，把当前选中作为生效
	if c.AppliedPresetID == "" && c.AppliedCustomImage == "" {
		updateConfig(func(cc *Config) {
			cc.AppliedPresetID = c.PresetID
			cc.AppliedCustomImage = c.CustomImage
		})
	}
	failed := 0
	for _, m := range mons {
		if !c.AllMonitors && !m.Primary {
			continue
		}
		ow, err := createOneOverlay(m)
		if err != nil {
			logf("ERROR: 创建遮罩失败 (%s): %v", m.Device, err)
			failed++
			continue
		}
		activeOverlays = append(activeOverlays, ow)
	}

	if len(activeOverlays) == 0 {
		logf("ERROR: 所有显示器的遮罩创建均失败")
		return
	}

	overlayOn = true
	notifySettingsStateChanged() // 面板开着时实时刷新（徽章/按钮状态）
	// 150ms 抢一次置顶：右键菜单/输入法窗这类系统置顶 UI 新弹出时会盖在遮罩上，
	// 加快抢回频率能把它们"一闪而过"的可见时间压到最低。
	procSetTimer.Call(uintptr(controlHwnd), TIMER_KEEPTOP, 150, 0)
	hideCursors()
	// 不再冻结物理鼠标：UU 实测证明它只是隐藏光标、并不拦截事件，
	// 吞掉移动事件反而让本地点击落点与光标位置脱节。
	logf("INFO: 遮罩已开启，共 %d 块屏 (失败 %d, 线程=%d/UI=%d)", len(activeOverlays), failed, curThreadID(), uiThreadID)
	messageBeep()
}

// disableOverlay 销毁所有遮罩窗口。必须在 UI 线程调用。
func disableOverlay() {
	if !inUIThread() {
		logf("WARNING: disableOverlay 被非 UI 线程调用(当前=%d, UI=%d)，已转发", curThreadID(), uiThreadID)
		postCmd(cmdDisable)
		return
	}
	if !overlayOn && len(activeOverlays) == 0 {
		return
	}
	procKillTimer.Call(uintptr(controlHwnd), TIMER_KEEPTOP)
	showCursors()
	for _, ow := range activeOverlays {
		procDestroyWindow.Call(uintptr(ow.hwnd))
		ow.releaseBitmap()
	}
	activeOverlays = nil
	overlayOn = false
	notifySettingsStateChanged() // 面板开着时实时刷新
	logf("INFO: 遮罩已关闭 (线程=%d/UI=%d)", curThreadID(), uiThreadID)
	messageBeep()
}

// rebuildOverlay 配置变化后重建（例如更换图片、切换屏数）。
func rebuildOverlay() {
	wasOn := overlayOn
	disableOverlay()
	if wasOn {
		// 让 DWM 有一帧时间清理，避免新窗口复用旧句柄
		enableOverlay()
	}
}

// keepOnTop 由定时器驱动，抵抗其他置顶窗口抢占与 DWM 状态重置。
func keepOnTop() {
	defer placeCursorWindow(0) // 遮罩排好后，把替身光标重新插到遮罩正下方
	for _, ow := range activeOverlays {
		procSetWindowPos.Call(
			uintptr(ow.hwnd), HWND_TOPMOST,
			0, 0, 0, 0,
			uintptr(SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE),
		)
		// 某些驱动在窗口重排后会丢失排除状态，定期重新声明
		setExcludeFromCapture(ow.hwnd)
	}
}

// ---------- 窗口过程 ----------

func overlayWndProc(hwnd windows.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_NCHITTEST:
		// 鼠标事件穿透到下方真实桌面：远程点击才能落到桌面图标/窗口上
		return HTTRANSPARENT
	case WM_MOUSEACTIVATE:
		return MA_NOACTIVATE
	case WM_ERASEBKGND:
		return 1 // 已自行绘制，禁止系统擦除，防闪烁
	case WM_PAINT:
		var ps PAINTSTRUCT
		hdc, _, _ := procBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
		for _, ow := range activeOverlays {
			if ow.hwnd == hwnd {
				ow.paint(hdc)
				break
			}
		}
		procEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
		return 0
	case WM_SYSCOMMAND:
		cmd := wParam & 0xFFF0
		// 屏幕被遮住时误按 Alt+F4 / 最小化会让用户彻底失去反馈，直接吞掉
		if cmd == SC_MINIMIZE || cmd == SC_CLOSE {
			return 0
		}
	case WM_DISPLAYCHANGE:
		// 拔插显示器 / 改分辨率 / 合盖：全部销毁重建
		logf("INFO: 检测到显示设置变化，重建遮罩")
		wasOn := overlayOn
		disableOverlay()
		if wasOn {
			enableOverlay()
		}
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return r
}

// ---------- 控制窗口 ----------

// createControlWindow 创建 message-only 窗口，作为托盘、热键与跨线程命令的统一入口。
func createControlWindow() error {
	hInst := getModuleHandle()
	wc := WNDCLASSEXW{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		LpfnWndProc:   controlWndProcCallback,
		HInstance:     hInst,
		LpszClassName: utf16Ptr(controlClassName),
	}
	r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return err
	}

	// 用普通隐藏窗口（不带 WS_VISIBLE）而不是 message-only 窗口：
	// FindWindowW 无法枚举 message-only 窗口，而"第二次启动通知旧实例退出"依赖它能被找到。
	hwnd, _, err := procCreateWindowExW.Call(
		WS_EX_TOOLWINDOW, // 不进 Alt+Tab、不显示在任务栏
		uintptr(unsafe.Pointer(utf16Ptr(controlClassName))),
		uintptr(unsafe.Pointer(utf16Ptr("VeilControl"))),
		0, 0, 0, 0, 0,
		0, 0, uintptr(hInst), 0,
	)
	if hwnd == 0 {
		return err
	}
	controlHwnd = windows.Handle(hwnd)
	// UIAccess 版进程的窗口受 UIPI 保护，便携版实例发来的退出通知会被系统丢弃
	// （症状："旧实例 3 秒内未退出"）。显式放行这一条消息，保住"再双击一次 exe 自救"。
	procChangeWindowMessageFilterEx.Call(hwnd, uintptr(cmdQuit), MSGFLT_ALLOW, 0)
	return nil
}

// 跨线程命令
const (
	cmdToggle    = WM_APP + 10
	cmdEnable    = WM_APP + 11
	cmdDisable   = WM_APP + 12
	cmdRebuild   = WM_APP + 13
	cmdQuit      = WM_APP + 14
	cmdRefreshUI = WM_APP + 15
	cmdPreview   = WM_APP + 16
	cmdSetHotkey  = WM_APP + 17
	cmdOpenSettings = WM_APP + 18
	cmdApply      = WM_APP + 19
)

const (
	// TIMER_PREVIEW 用于"在物理屏上试放 2.5 秒"
	TIMER_PREVIEW = 2
	// TIMER_PROBE 用于命令行自检
	TIMER_PROBE = 3
)

func postCmd(cmd uint32) {
	if controlHwnd == 0 {
		return
	}
	procPostMessageW.Call(uintptr(controlHwnd), uintptr(cmd), 0, 0)
}

func controlWndProc(hwnd windows.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_TRAYICON:
		ev := loword(lParam)
		// NOTIFYICON_VERSION_4 下一次右键会先后发 WM_RBUTTONUP 和 WM_CONTEXTMENU，
		// 两个都处理就会弹出两个菜单。v4 只认 WM_CONTEXTMENU / NIN_SELECT。
		if ev == WM_CONTEXTMENU {
			showTrayMenu()
		} else if ev == NIN_SELECT {
			// 左键单击托盘图标 = 直接打开设置面板（遮罩的开关用热键或右键菜单）
			go openSettingsWindow()
		}
		return 0

	case WM_HOTKEY:
		switch wParam {
		case idHotkeyToggle: // 开关遮罩（可自定义）
			postCmd(cmdToggle)
		case idHotkeyQuit, idHotkeyEscape: // 退出（可自定义 + 固定兜底）
			postCmd(cmdQuit)
		case idHotkeySettings: // 打开设置面板
			postCmd(cmdOpenSettings)
		}
		return 0

	case cmdSetHotkey:
		// 前端请求修改热键：处理完把结果写回请求对象
		hkMu.Lock()
		req := pendingHK
		pendingHK = nil
		hkMu.Unlock()
		if req != nil {
			req.result <- applySetHotkey(req.kind, req.combo)
			close(req.result)
		}
		return 0

	case cmdToggle:
		if overlayOn {
			disableOverlay()
		} else {
			enableOverlay()
		}
		updateTrayTip()
		return 0

	case cmdEnable:
		enableOverlay()
		updateTrayTip()
		return 0

	case cmdDisable:
		disableOverlay()
		updateTrayTip()
		return 0

	case cmdRebuild:
		rebuildOverlay()
		updateTrayTip()
		return 0

	case cmdQuit:
		disableOverlay()
		removeTrayIcon()
		procDestroyWindow.Call(uintptr(hwnd))
		procPostQuitMessage.Call(0)
		return 0

	case cmdApply:
		// 应用选中配置：快照"生效"状态，然后重建遮罩（已开）或开启（未开）
		c := getConfig()
		updateConfig(func(cc *Config) {
			cc.AppliedPresetID = c.PresetID
			cc.AppliedCustomImage = c.CustomImage
		})
		if overlayOn {
			rebuildOverlay()
		} else {
			enableOverlay()
		}
		updateTrayTip()
		return 0

	case cmdOpenSettings:
		// 热键触发：面板开着时按 = 关闭；没开时按 = 打开（开关切换）
		// 打开会派生子线程创建 WebView2，必须异步避免阻塞 UI 线程
		if settingsVisible {
			hideSettings()
		} else {
			go openSettingsWindow()
		}
		return 0

	case cmdPreview:
		// 在物理屏上试放"当前选中"的图 2.5 秒。
		// 遮罩已开时也要预览（早期版本这里 if !overlayOn 直接跳过，
		// 导致遮罩开着点预览毫无反应）。
		c := getConfig()
		renderOverride.Store(&renderSpec{PresetID: c.PresetID, CustomImage: c.CustomImage})
		previewWasOn = overlayOn
		if overlayOn {
			rebuildOverlay() // 用选中配置重建
		} else {
			enableOverlay()
		}
		procSetTimer.Call(uintptr(controlHwnd), TIMER_PREVIEW, 2500, 0)
		return 0

	case WM_TIMER:
		if wParam == TIMER_KEEPTOP {
			keepOnTop()
		} else if wParam == TIMER_CURSOR {
			syncCursorWindow()
		} else if wParam == TIMER_PREVIEW {
			procKillTimer.Call(uintptr(controlHwnd), TIMER_PREVIEW)
			renderOverride.Store(nil) // 清掉预览临时配置
			if previewWasOn {
				// 预览前遮罩是开的：恢复到"生效配置"显示
				rebuildOverlay()
			} else {
				disableOverlay()
			}
		} else if wParam == TIMER_PROBE {
			procKillTimer.Call(uintptr(controlHwnd), TIMER_PROBE)
			disableOverlay()
			logf("INFO: 自检结束")
			postCmd(cmdQuit)
		}
		return 0

	case WM_DESTROY:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return r
}

// ---------- 位图辅助 ----------

// solidImage 生成纯色图像（预览等场景使用）。
func solidImage(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i] = c.R
		img.Pix[i+1] = c.G
		img.Pix[i+2] = c.B
		img.Pix[i+3] = 255
	}
	return img
}


func curThreadID() uint32 {
	tid, _, _ := procGetCurrentThreadId.Call()
	return uint32(tid)
}


// notifySettingsStateChanged 让设置面板重新拉取状态（遮罩开关后徽章/按钮要立即变）。
// 前端状态是 boot() 时的快照，不主动刷新就只能关面板再开才更新。
func notifySettingsStateChanged() {
	settingsMu.Lock()
	w := settingsWV
	visible := settingsVisible
	settingsMu.Unlock()
	if w == nil || !visible {
		return
	}
	w.Dispatch(func() {
		w.Eval("refreshState()")
	})
}
