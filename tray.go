// tray.go — 系统托盘图标与右键菜单。
// 托盘是程序唯一常驻可见的入口：遮罩开启后屏幕全被盖住，用户靠托盘和热键控制。
package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	trayAdded bool
	trayIcon  windows.Handle
)

// appIconResID 返回 exe 内嵌图标的资源 ID（找不到返回 0）。
// rsrc 按顺序分配 ID：便携版只有图标（ID 1），UIAccess 版 manifest 占 ID 1、图标是 ID 2。
func appIconResID() uintptr {
	for id := uintptr(1); id <= 3; id++ {
		if h, _, _ := procLoadIconW.Call(uintptr(getModuleHandle()), id); h != 0 {
			return id
		}
	}
	return 0
}

func initTrayIcon() {
	// 优先从 exe 内嵌资源取图标
	var r uintptr
	if id := appIconResID(); id != 0 {
		r, _, _ = procLoadIconW.Call(uintptr(getModuleHandle()), id)
	}
	if r == 0 {
		// 兜底：系统默认图标
		r, _, _ = procLoadIconW.Call(0, uintptr(IDI_APPLICATION))
	}
	trayIcon = windows.Handle(r)
}

func trayData() NOTIFYICONDATAW {
	var nid NOTIFYICONDATAW
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = controlHwnd
	nid.UID = 1
	nid.UFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP
	nid.UCallbackMessage = WM_TRAYICON
	nid.HIcon = trayIcon
	copy(nid.SzTip[:], syscall.StringToUTF16("Veil 隐幕")[0:])
	return nid
}

func addTrayIcon() error {
	if trayAdded {
		return nil
	}
	nid := trayData()
	r, _, err := procShellNotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(&nid)))
	if r == 0 {
		return err
	}
	// 使用 v4 版本行为，才能收到 WM_CONTEXTMENU 而不是旧的 WM_RBUTTONUP
	nid.UVersion = NOTIFYICON_VERSION_4
	procShellNotifyIconW.Call(NIM_SETVERSION, uintptr(unsafe.Pointer(&nid)))
	trayAdded = true
	return nil
}

func removeTrayIcon() {
	if !trayAdded {
		return
	}
	nid := trayData()
	procShellNotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&nid)))
	trayAdded = false
}

// showTrayBalloon 弹一条托盘气泡通知，给用户"程序确实起来了"的即时反馈。
func showTrayBalloon(title, msg string) {
	if !trayAdded {
		return
	}
	nid := trayData()
	nid.UFlags |= NIF_INFO
	copyUTF16(nid.SzInfoTitle[:], title)
	copyUTF16(nid.SzInfo[:], msg)
	nid.DwInfoFlags = 0x00000001 // NIIF_INFO
	procShellNotifyIconW.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&nid)))
}

// copyUTF16 安全拷贝字符串到固定长度 UTF-16 缓冲区，保证以 0 结尾且不越界。
func copyUTF16(dst []uint16, s string) {
	if len(dst) == 0 {
		return
	}
	src, err := syscall.UTF16FromString(s)
	if err != nil || len(src) == 0 {
		dst[0] = 0
		return
	}
	n := len(src)
	if n > len(dst) {
		n = len(dst)
	}
	copy(dst[:n], src[:n])
	dst[n-1] = 0
}

func updateTrayTip() {
	if !trayAdded {
		return
	}
	nid := trayData()
	combo, active, disabled := toggleHotkeyState()
	state, action := "未开启", "开启"
	if overlayOn {
		state, action = "防窥已开启", "关闭"
	}
	tip := fmt.Sprintf("Veil 隐幕 — %s (%s %s)", state, combo, action)
	if disabled {
		tip = fmt.Sprintf("Veil 隐幕 — %s", state)
	} else if !active {
		tip = fmt.Sprintf("Veil 隐幕 — %s（开关热键未生效）", state)
	}
	copy(nid.SzTip[:], syscall.StringToUTF16(tip)[0:])
	procShellNotifyIconW.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&nid)))
}

// showTrayMenu 在光标处弹出菜单。
// 结构对齐 UU 远程：不启用 / 隐私屏保（圆点单选）+ 设置入口。
func showTrayMenu() {
	hMenu, _, _ := procCreatePopupMenu.Call()
	if hMenu == 0 {
		return
	}
	defer procDestroyMenu.Call(hMenu)

	// 选中标记用默认勾号（√）；如需圆点样式，给 flags 加 MFT_RADIOCHECK
	addRadio := func(id uintptr, text string, selected bool) {
		flags := uintptr(MF_STRING)
		if selected {
			flags |= MF_CHECKED
		}
		procAppendMenuW.Call(hMenu, flags, id, uintptr(unsafe.Pointer(utf16Ptr(text))))
	}
	addItem := func(id uintptr, text string) {
		procAppendMenuW.Call(hMenu, MF_STRING, id, uintptr(unsafe.Pointer(utf16Ptr(text))))
	}

	addRadio(CMD_DISABLE, "不启用", !overlayOn)
	addRadio(CMD_ENABLE, "隐私屏保", overlayOn)
	procAppendMenuW.Call(hMenu, MF_SEPARATOR, 0, 0)
	addItem(CMD_SETTINGS, "设置隐私屏保")
	addItem(CMD_OPENLOG, "查看日志")
	// 一键禁用 / 恢复全部三组热键；与设置面板里每组的"禁用"是同一份状态
	addRadio(CMD_HOTKEYSOFF, "禁用快捷键", allHotkeysOff())
	procAppendMenuW.Call(hMenu, MF_SEPARATOR, 0, 0)
	addItem(CMD_EXIT, "退出")

	// 菜单项图标：当前隐藏。
	// 图标文件为 assets/menu/eye.png（tools/genmenu 从 SVG 栅格化生成，32x32 白底），
	// 构建逻辑在下方 makeEyeIcon()。后期如需开启（或做成 config 可配置项），
	// 取消下面注释即可：
	// if eye := makeEyeIcon(); eye != 0 {
	// 	defer procDeleteObject.Call(uintptr(eye))
	// 	var mii MENUITEMINFOW
	// 	mii.CbSize = uint32(unsafe.Sizeof(mii))
	// 	mii.FMask = MIIM_BITMAP
	// 	mii.HbmpItem = eye
	// 	procSetMenuItemInfoW.Call(hMenu, uintptr(CMD_SETTINGS), 0, uintptr(unsafe.Pointer(&mii)))
	// }

	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	// 必须先置前台，否则点击菜单外部时菜单不会关闭
	procSetForegroundWindow.Call(uintptr(controlHwnd))

	cmd, _, _ := procTrackPopupMenu.Call(
		hMenu,
		uintptr(TPM_LEFTALIGN|TPM_RIGHTBUTTON|TPM_RETURNCMD),
		uintptr(pt.X), uintptr(pt.Y),
		0,
		uintptr(controlHwnd),
		0,
	)

	// TrackPopupMenu 返回后必须投递一个空消息，否则菜单的模态循环不会退出、菜单残留
	procPostMessageW.Call(uintptr(controlHwnd), 0x0000, 0, 0)

	switch uint32(cmd) {
	case CMD_ENABLE:
		postCmd(cmdEnable)
	case CMD_DISABLE:
		postCmd(cmdDisable)
	case CMD_OPENLOG:
		openLogLocation()
		return
	case CMD_HOTKEYSOFF:
		applySetAllHotkeysOff(!allHotkeysOff())

	case CMD_SETTINGS:
		go openSettingsWindow() // 异步：创建会阻塞
	case CMD_EXIT:
		postCmd(cmdQuit)
	}
}

// makeEyeIcon 从嵌入的 eye.png 构建 HBITMAP。
// 图标文件是离线用 tools/genmenu 从用户提供的 SVG 栅格化的（32x32 白底），
// 白底的原因：SetMenuItemInfo 的 hbmpItem 不支持 per-pixel alpha，
// 透明像素会被菜单渲染成黑色。
//
//go:embed assets/menu/eye.png
var eyeIconPNG []byte

func makeEyeIcon() windows.Handle {
	img, err := png.Decode(bytes.NewReader(eyeIconPNG))
	if err != nil {
		logf("WARNING: 菜单图标解码失败: %v", err)
		return 0
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	hdcScreen, _, _ := procGetDC.Call(0)
	defer procReleaseDC.Call(0, hdcScreen)

	var bi BITMAPINFO
	bi.BmiHeader.BiSize = uint32(unsafe.Sizeof(BITMAPINFOHEADER{}))
	bi.BmiHeader.BiWidth = int32(w)
	bi.BmiHeader.BiHeight = -int32(h)
	bi.BmiHeader.BiPlanes = 1
	bi.BmiHeader.BiBitCount = 32
	bi.BmiHeader.BiCompression = BI_RGB

	var bits unsafe.Pointer
	hbmp, _, _ := procCreateDIBSection.Call(
		hdcScreen, uintptr(unsafe.Pointer(&bi)), DIB_RGB_COLORS,
		uintptr(unsafe.Pointer(&bits)), 0, 0,
	)
	if hbmp == 0 || bits == nil {
		return 0
	}

	// png.Decode 返回的可能是 *image.NRGBA（PNG 的原生格式），
	// 不能硬断言成 *image.RGBA——类型不符会 panic 并让整个程序崩溃
	// （用户右键托盘 → 崩溃 → 遮罩如果开着会直接消失）。
	// 统一用 draw.Draw 转成 *image.RGBA 再搬运。
	srcImg := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(srcImg, srcImg.Bounds(), img, b.Min, draw.Src)

	buf := unsafe.Slice((*byte)(bits), w*h*4)
	n := 0
	for y := 0; y < h; y++ {
		row := srcImg.Pix[y*srcImg.Stride:]
		for x := 0; x < w; x++ {
			// RGBA → BGRA；源图白底全不透明，直接搬运
			buf[n+0] = row[x*4+2]
			buf[n+1] = row[x*4+1]
			buf[n+2] = row[x*4+0]
			buf[n+3] = 255
			n += 4
		}
	}

	return windows.Handle(hbmp)
}

// openLogLocation 用资源管理器打开日志所在文件夹并选中文件：
// 想看内容双击、想发给别人右键复制路径、想换编辑器也方便。
func openLogLocation() {
	path := logFilePath()
	if _, err := os.Stat(path); err != nil {
		showTrayBalloon("Veil", "日志文件还不存在（程序尚未产生日志）")
		return
	}
	shell32 := windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteW := shell32.NewProc("ShellExecuteW")

	verb, _ := syscall.UTF16PtrFromString("open")
	file, _ := syscall.UTF16PtrFromString("explorer.exe")
	params, _ := syscall.UTF16PtrFromString(`/select,"` + path + `"`)
	procShellExecuteW.Call(0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		uintptr(unsafe.Pointer(params)),
		0, SW_SHOWNORMAL)
	logf("INFO: 已打开日志位置 %s", path)
}

// openFolder 在资源管理器里打开目录（不存在则先创建）。
func openFolder(path string) {
	if err := os.MkdirAll(path, 0755); err != nil {
		logf("WARNING: 创建目录失败 %s: %v", path, err)
	}
	shellExecuteOpen(path)
	logf("INFO: 已打开文件夹 %s", path)
}

// openLogFile 用系统默认程序打开日志文件。
func openLogFile() {
	path := logFilePath()
	if _, err := os.Stat(path); err != nil {
		logf("INFO: 日志文件尚不存在，无法打开")
		return
	}
	shellExecuteOpen(path)
	logf("INFO: 已打开日志文件 %s", path)
}

func shellExecuteOpen(path string) {
	verb, _ := syscall.UTF16PtrFromString("open")
	file, _ := syscall.UTF16PtrFromString(path)
	procShellExecuteW.Call(0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		0, 0, SW_SHOWNORMAL)
}
