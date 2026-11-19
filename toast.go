// toast.go — 自绘的轻量提示条。
//
// 不用 Shell_NotifyIcon 的气泡：Win11 要求 AppUserModelID，没有就静默丢弃，
// 而且会被"专注助手/勿扰"拦截。自己画一个窗口，100% 可控。
package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

const toastClassName = "VeilToastWindow"

const (
	TIMER_TOAST = 5
)

var (
	toastW, toastH   int
	toastScale       float64 = 1
	fsTitle, fsSub   int32
	toastWndProcCb   uintptr
	toastTitle       string
	toastSub         string
	toastHWND        windows.Handle
	toastRegistered  bool
)

func registerToastClass() {
	if toastRegistered {
		return
	}
	wc := WNDCLASSEXW{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		LpfnWndProc:   toastWndProcCb,
		HInstance:     getModuleHandle(),
		LpszClassName: utf16Ptr(toastClassName),
	}
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	toastRegistered = true
}

// showToast 在屏幕底部居中弹出一个提示条，2.8 秒后自动消失。
// 必须在 UI 线程调用（窗口与其定时器归属该线程的消息循环）。
func showToast(title, sub string) {
	if toastHWND != 0 {
		procDestroyWindow.Call(uintptr(toastHWND))
		toastHWND = 0
	}
	toastTitle, toastSub = title, sub
	registerToastClass()

	// 按系统 DPI 缩放：窗口与字体都是物理像素，200% 缩放下不放大就会小一半
	dpi, _, _ := procGetDpiForSystem.Call()
	scale := float64(dpi) / 96.0
	if scale < 1 {
		scale = 1
	}
	toastW = int(380 * scale)
	toastH = int(88 * scale)
	toastScale = scale
	fsTitle = int32(float64(-17) * scale)
	fsSub = int32(float64(-12.5) * scale)

	// 用工作区（自动排除任务栏），否则气泡会压住任务栏
	var wa RECT
	got := false
	if r, _, _ := procSystemParametersInfoW.Call(SPI_GETWORKAREA, 0, uintptr(unsafe.Pointer(&wa)), 0); r != 0 {
		got = true
	}
	if !got || wa.Right <= 0 || wa.Bottom <= 0 {
		sw, _, _ := procGetSystemMetrics.Call(0)
		sh, _, _ := procGetSystemMetrics.Call(1)
		wa = RECT{0, 0, int32(sw), int32(sh)}
	}
	x := int(wa.Right) - toastW - int(10*scale)
	y := int(wa.Bottom) - toastH - int(10*scale)
	if x < int(wa.Left) {
		x = int(wa.Left)
	}
	if y < int(wa.Top) {
		y = int(wa.Top)
	}

	exStyle := uintptr(WS_EX_TOPMOST | WS_EX_TOOLWINDOW | WS_EX_NOACTIVATE)
	hwnd, _, _ := procCreateWindowExW.Call(
		exStyle,
		uintptr(unsafe.Pointer(utf16Ptr(toastClassName))),
		uintptr(unsafe.Pointer(utf16Ptr("Veil"))),
		WS_POPUP,
		uintptr(x), uintptr(y),
		uintptr(toastW), uintptr(toastH),
		0, 0, uintptr(getModuleHandle()), 0,
	)
	if hwnd == 0 {
		logf("WARNING: 提示条创建失败")
		return
	}
	toastHWND = windows.Handle(hwnd)

	// 圆角：用区域裁切
	rgn, _, _ := procCreateRoundRectRgn.Call(0, 0, uintptr(toastW+1), uintptr(toastH+1), 18, 18)
	if rgn != 0 {
		procSetWindowRgn.Call(hwnd, rgn, 1)
	}

	procShowWindow.Call(hwnd, SW_SHOWNOACTIVATE)
	procSetTimer.Call(hwnd, TIMER_TOAST, 2800, 0)
}

func toastWndProc(hwnd windows.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_ERASEBKGND:
		return 1
	case WM_PAINT:
		paintToast(hwnd)
		return 0
	case WM_TIMER:
		if wParam == TIMER_TOAST {
			procKillTimer.Call(uintptr(hwnd), TIMER_TOAST)
			procDestroyWindow.Call(uintptr(hwnd))
		}
		return 0
	case WM_DESTROY:
		if toastHWND == hwnd {
			toastHWND = 0
		}
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return r
}

func paintToast(hwnd windows.Handle) {
	var ps PAINTSTRUCT
	hdc, _, _ := procBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))

	// 背景 #1c1c1e
	brush, _, _ := procCreateSolidBrush.Call(uintptr(0x001e1c1c))
	if brush != 0 {
		rc := RECT{0, 0, int32(toastW), int32(toastH)}
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), brush)
		procDeleteObject.Call(brush)
	}

	procSetBkMode.Call(hdc, TRANSPARENT)

	// 标题
	fontTitle, _, _ := procCreateFontW.Call(
		uintptr(fsTitle),
		0, 0, 0, uintptr(600), 0, 0, 0,
		uintptr(1), 0, 0, 0, 0,
		uintptr(unsafe.Pointer(utf16Ptr("Microsoft YaHei UI"))),
	)
	if fontTitle != 0 {
		old, _, _ := procSelectObject.Call(hdc, fontTitle)
		procSetTextColor.Call(hdc, uintptr(0x00ffffff))
		// 绘制区域必须随 DPI 缩放：200% 下标题字高 40 物理像素，
		// 硬编码 22→56 的区域只有 34px，字被裁掉一半
		tr := RECT{
			int32(toastW / 10),
			int32(10 * toastScale),
			int32(toastW * 9 / 10),
			int32(48 * toastScale),
		}
		procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(utf16Ptr(toastTitle))), ^uintptr(0),
			uintptr(unsafe.Pointer(&tr)), uintptr(DT_CENTER|DT_SINGLELINE|DT_VCENTER))
		procSelectObject.Call(hdc, old)
		procDeleteObject.Call(fontTitle)
	}

	// 副标题
	fontSub, _, _ := procCreateFontW.Call(
		uintptr(fsSub),
		0, 0, 0, uintptr(400), 0, 0, 0,
		uintptr(1), 0, 0, 0, 0,
		uintptr(unsafe.Pointer(utf16Ptr("Microsoft YaHei UI"))),
	)
	if fontSub != 0 {
		old, _, _ := procSelectObject.Call(hdc, fontSub)
		procSetTextColor.Call(hdc, uintptr(0x00a8a29a))
		sr := RECT{
			int32(toastW / 10),
			int32(48 * toastScale),
			int32(toastW * 9 / 10),
			int32(toastH - int(10*toastScale)),
		}
		procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(utf16Ptr(toastSub))), ^uintptr(0),
			uintptr(unsafe.Pointer(&sr)), uintptr(DT_CENTER|DT_SINGLELINE|DT_VCENTER))
		procSelectObject.Call(hdc, old)
		procDeleteObject.Call(fontSub)
	}
}
