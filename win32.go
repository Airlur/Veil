// win32.go — 所有 Windows API 声明、常量与结构体，集中管理。
// 本项目刻意不引入任何 GUI 框架：遮罩窗口必须是"干净的一层"，
// 框架额外包装的窗口层会让 SetWindowDisplayAffinity 的排除效果失效。
package main

import (
	"syscall"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
)

var (
	procCreateWindowExW   = user32.NewProc("CreateWindowExW")
	procRegisterClassExW  = user32.NewProc("RegisterClassExW")
	procDefWindowProcW    = user32.NewProc("DefWindowProcW")
	procDestroyWindow     = user32.NewProc("DestroyWindow")
	procShowWindow        = user32.NewProc("ShowWindow")
	procUpdateWindow      = user32.NewProc("UpdateWindow")
	procSetWindowPos      = user32.NewProc("SetWindowPos")
	procGetMessageW       = user32.NewProc("GetMessageW")
	procTranslateMessage  = user32.NewProc("TranslateMessage")
	procDispatchMessageW  = user32.NewProc("DispatchMessageW")
	procPostQuitMessage   = user32.NewProc("PostQuitMessage")
	procPostMessageW      = user32.NewProc("PostMessageW")
	procSendMessageW      = user32.NewProc("SendMessageW")
	procInvalidateRect    = user32.NewProc("InvalidateRect")
	procGetClientRect     = user32.NewProc("GetClientRect")
	procSetTimer          = user32.NewProc("SetTimer")
	procKillTimer         = user32.NewProc("KillTimer")
	procRegisterHotKey    = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey  = user32.NewProc("UnregisterHotKey")
	procGetSystemMetrics  = user32.NewProc("GetSystemMetrics")
	procLoadIconW         = user32.NewProc("LoadIconW")
	procLoadCursorW       = user32.NewProc("LoadCursorW")
	procCreatePopupMenu   = user32.NewProc("CreatePopupMenu")
	procAppendMenuW       = user32.NewProc("AppendMenuW")
	procTrackPopupMenu    = user32.NewProc("TrackPopupMenu")
	procDestroyMenu       = user32.NewProc("DestroyMenu")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procGetCursorPos      = user32.NewProc("GetCursorPos")
	procEnumDisplayMonitors = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW   = user32.NewProc("GetMonitorInfoW")
	procGetDC             = user32.NewProc("GetDC")
	procReleaseDC         = user32.NewProc("ReleaseDC")
	procSetWindowDisplayAffinity = user32.NewProc("SetWindowDisplayAffinity")
	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	procFillRect          = user32.NewProc("FillRect")
	procMessageBeep       = user32.NewProc("MessageBeep")
	procBeginPaint        = user32.NewProc("BeginPaint")
	procEndPaint          = user32.NewProc("EndPaint")
	procFindWindowW       = user32.NewProc("FindWindowW")
	procLoadImageW        = user32.NewProc("LoadImageW")
	procGetWindowRect     = user32.NewProc("GetWindowRect")
)

var (
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procPostThreadMessageW  = user32.NewProc("PostThreadMessageW")
	procSetWindowRgn        = user32.NewProc("SetWindowRgn")
	procCreateRoundRectRgn  = gdi32.NewProc("CreateRoundRectRgn")
	procCreateFontW         = gdi32.NewProc("CreateFontW")
	procSetBkMode           = gdi32.NewProc("SetBkMode")
	procSetTextColor        = gdi32.NewProc("SetTextColor")
	procDrawTextW           = user32.NewProc("DrawTextW")
	procGetSystemMetrics2   = user32.NewProc("GetSystemMetrics")
	procGetCurrentThreadId  = kernel32.NewProc("GetCurrentThreadId")
	procLoadImageFromRes    = user32.NewProc("LoadImageW")
	procGetStockObject      = gdi32.NewProc("GetStockObject")
	procSetMenuItemInfoW    = user32.NewProc("SetMenuItemInfoW")
	procSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	procGetDpiForSystem     = user32.NewProc("GetDpiForSystem")
	procSetWindowLongPtrW2  = user32.NewProc("SetWindowLongPtrW")
	procCallWindowProcW     = user32.NewProc("CallWindowProcW")
	procSystemParametersInfoW = user32.NewProc("SystemParametersInfoW")
	procGetSysColor         = user32.NewProc("GetSysColor")
	procCreatePen           = gdi32.NewProc("CreatePen")
	procRoundRect           = gdi32.NewProc("RoundRect")
	procPolyline            = gdi32.NewProc("Polyline")
	procSetPixel            = gdi32.NewProc("SetPixel")
)

var (
	// GetModuleHandleW 属于 kernel32（不是 user32）
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
)

var (
	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procCreateDIBSection       = gdi32.NewProc("CreateDIBSection")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
)

var (
	procShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	procCreateMutexW     = kernel32.NewProc("CreateMutexW")
	procGetLastError     = kernel32.NewProc("GetLastError")
)

// ---------- 常量 ----------

const (
	WS_POPUP       = 0x80000000
	WS_VISIBLE     = 0x10000000
	WS_EX_TOPMOST  = 0x00000008
	WS_EX_TOOLWINDOW = 0x00000080
	WS_EX_NOACTIVATE = 0x08000000
	WS_EX_TRANSPARENT = 0x00000020
	WS_EX_LAYERED     = 0x00080000

	// LWA_ALPHA：分层窗口激活标志。WS_EX_LAYERED 窗口在调用
	// SetLayeredWindowAttributes 之前不会渲染任何内容。
	LWA_ALPHA = 0x00000002

	SW_SHOWNOACTIVATE = 4
	SW_SHOWNORMAL     = 1
	SW_HIDE           = 0

	HWND_TOPMOST   = ^uintptr(0) // (HWND)-1
	HWND_NOTOPMOST = ^uintptr(1) // (HWND)-2

	SWP_NOSIZE     = 0x0001
	SWP_NOMOVE     = 0x0002
	SWP_NOZORDER   = 0x0004
	SWP_NOACTIVATE = 0x0010
	SWP_SHOWWINDOW = 0x0040

	// WDA_EXCLUDEFROMCAPTURE 让窗口在物理屏可见，但在所有系统采集路径（DXGI/WGC/BitBlt）里被剔除。
	// 这是整个项目的核心。Windows 10 2004 (build 19041) 起才有。
	WDA_NONE              = 0x00000000
	WDA_MONITOR           = 0x00000001
	WDA_EXCLUDEFROMCAPTURE = 0x00000011
)

const (
	WM_DESTROY      = 0x0002
	WM_CLOSE        = 0x0010
	WM_PAINT        = 0x000F
	WM_ERASEBKGND   = 0x0014
	WM_NCHITTEST    = 0x0084
	WM_MOUSEACTIVATE = 0x0021
	WM_TIMER        = 0x0113
	WM_HOTKEY       = 0x0312
	WM_SYSCOMMAND   = 0x0112
	WM_DISPLAYCHANGE = 0x007E
	WM_APP          = 0x8000
	WM_TRAYICON     = WM_APP + 1

	WM_RBUTTONUP = 0x0205
	WM_LBUTTONUP = 0x0202
	WM_CONTEXTMENU = 0x007B
	NIN_SELECT     = 0x0400
	WM_MOUSEMOVE   = 0x0200
	WM_QUIT        = 0x0012

	SC_MINIMIZE = 0xF020
	SC_CLOSE    = 0xF060

	HTTRANSPARENT = ^uintptr(0) // -1
	MA_NOACTIVATE = 3
)

const (
	MOD_ALT      = 0x0001
	MOD_CONTROL  = 0x0002
	MOD_SHIFT    = 0x0004
	MOD_NOREPEAT = 0x4000

	VK_V = 0x56
	VK_Q = 0x51
)

const (
	NIM_ADD        = 0x00000000
	NIM_MODIFY     = 0x00000001
	NIM_DELETE     = 0x00000002
	NIM_SETVERSION = 0x00000004

	NIF_MESSAGE = 0x00000001
	NIF_ICON    = 0x00000002
	NIF_TIP     = 0x00000004
	NIF_INFO    = 0x00000010

	NOTIFYICON_VERSION_4 = 4
)

const (
	MF_STRING    = 0x00000000
	MF_SEPARATOR = 0x00000800
	MF_CHECKED   = 0x00000008
	MF_GRAYED    = 0x00000001

	// MFT_RADIOCHECK 让选中标记显示为圆点而非勾号（单选语义）
	MFT_RADIOCHECK = 0x00000200
	MIIM_BITMAP    = 0x00000080
	COLOR_MENU     = 4

	TPM_LEFTALIGN   = 0x0000
	TPM_RIGHTBUTTON = 0x0002
	TPM_RETURNCMD   = 0x0100
)

const (
	SRCCOPY       = 0x00CC0020
	DIB_RGB_COLORS = 0
	BI_RGB        = 0
)

const (
	IDI_APPLICATION = 32512
	IDC_ARROW       = 32512
)

const (
	GWLP_WNDPROC = ^uintptr(3) // -4
	SPI_GETWORKAREA = 0x0030
	DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = ^uintptr(3) // -4
	MONITORINFOF_PRIMARY = 0x00000001
)

const (
	WH_MOUSE_LL = 14
	HC_ACTION   = 0

	// LLMHF_INJECTED 标记该输入来自 SendInput/mouse_event 等程序注入。
	// 远控软件注入的鼠标事件带这个标志，物理鼠标不带——据此区分"本地人动鼠标"和"远程操作"。
	LLMHF_INJECTED = 0x00000001
)

const (
	MB_ICONASTERISK = 0x00000040
	MB_OK           = 0
)

const (
	DT_CENTER     = 0x00000001
	DT_VCENTER    = 0x00000004
	DT_SINGLELINE = 0x00000020
	TRANSPARENT   = 1
)

// 菜单命令
const (
	CMD_ENABLE   = 1001
	CMD_DISABLE  = 1002
	CMD_SETTINGS = 1003
	CMD_EXIT     = 1004
	CMD_OPENLOG  = 1005
)

const (
	TIMER_KEEPTOP = 1
)

// ---------- 结构体 ----------

type POINT struct {
	X, Y int32
}

// MSLLHOOKSTRUCT 是低级鼠标钩子的参数结构。
type MSLLHOOKSTRUCT struct {
	Pt          POINT
	MouseData   uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

type RECT struct {
	Left, Top, Right, Bottom int32
}

type MSG struct {
	Hwnd    windows.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
	Private uint32
}

type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     windows.Handle
	HIcon         windows.Handle
	HCursor       windows.Handle
	HbrBackground windows.Handle
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       windows.Handle
}

type MONITORINFOEXW struct {
	CbSize    uint32
	RcMonitor RECT
	RcWork    RECT
	DwFlags   uint32
	SzDevice  [32]uint16
}

type NOTIFYICONDATAW struct {
	CbSize           uint32
	HWnd             windows.Handle
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            windows.Handle
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         windows.GUID
	HBalloonIcon     windows.Handle
}

// PAINTSTRUCT 与 Win32 结构布局一致。
// MENUITEMINFOW 用于给菜单项挂自绘位图图标。
type MENUITEMINFOW struct {
	CbSize       uint32
	FMask        uint32
	FType        uint32
	FState       uint32
	WID          uint32
	HSubMenu     windows.Handle
	HbmpChecked  windows.Handle
	HbmpUnchecked windows.Handle
	DwItemData   uintptr
	DwTypeData   *uint16
	Cch          uint32
	HbmpItem     windows.Handle
}

type PAINTSTRUCT struct {
	Hdc         windows.Handle
	FErase      int32
	RcPaint     RECT
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}

type BITMAPINFOHEADER struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

type BITMAPINFO struct {
	BmiHeader BITMAPINFOHEADER
	BmiColors [1]uint32
}

// ---------- 辅助函数 ----------

func utf16Ptr(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		return nil
	}
	return p
}

func utf16ToString(buf []uint16) string {
	// 去掉尾部 0
	n := 0
	for n < len(buf) && buf[n] != 0 {
		n++
	}
	return syscall.UTF16ToString(buf[:n])
}

func loword(v uintptr) uint16 { return uint16(v & 0xFFFF) }
func hiword(v uintptr) uint16 { return uint16((v >> 16) & 0xFFFF) }

func getModuleHandle() windows.Handle {
	h, _, _ := procGetModuleHandleW.Call(0)
	return windows.Handle(h)
}

func messageBeep() {
	procMessageBeep.Call(0xFFFFFFFF)
}
