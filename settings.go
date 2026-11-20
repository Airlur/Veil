// settings.go — 设置窗口（WebView2 + 内嵌 HTML）。
//
// 实例常驻 + UI 线程模式：WebView2 必须创建在控制窗口所在的 UI 线程上
// （早期版本放在独立线程，其消息循环退出时的 PostQuitMessage 会击穿
// 主 UI 线程的消息循环——表现为热键失灵、托盘图标消失、进程假死）。
// 主 UI 消息循环本身就泵 WebView2 的消息，因此这里绝不调用 w.Run()。
// 关闭 = 隐藏窗口（实例保留，二次打开秒出）；销毁实例也安全，不影响主循环。
package main

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
)

//go:embed ui/index.html
var settingsHTML string

var (
	settingsWV      webview2.WebView
	settingsMu      sync.Mutex
	settingsVisible bool

	// 窗口子类化：拦截设置窗的 WM_CLOSE（点标题栏 × / Alt+F4 / 任务栏关闭）
	// 改为隐藏到托盘——webview 库收到 WM_CLOSE 会 DestroyWindow + PostQuitMessage，
	// 那会击穿主 UI 线程导致整个程序退出。
	settingsOrigWndProc uintptr
	settingsProcCb      uintptr
)

const wmCloseConst = 0x0010

// 设置窗口的子类化窗口过程
func settingsSubclassedProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	if msg == wmCloseConst {
		hideSettings() // 关闭 = 隐藏到托盘
		return 0
	}
	r, _, _ := procCallWindowProcW.Call(settingsOrigWndProc, hwnd, msg, wParam, lParam)
	return r
}

// SettingsState 是传给前端的完整状态。
type SettingsState struct {
	Presets     []Preset `json:"presets"`
	PresetID    string   `json:"presetId"`
	AppliedPresetID    string `json:"appliedPresetId"`
	AppliedCustomImage string `json:"appliedCustomImage"`
	OverlayOn          bool   `json:"overlayOn"`
	CustomImage string   `json:"customImage"`
	CustomName  string   `json:"customName"`
	AllMonitors bool     `json:"allMonitors"`
	Version     string   `json:"version"`
}

func buildState() SettingsState {
	c := getConfig()
	name := ""
	if c.CustomImage != "" {
		name = filepath.Base(c.CustomImage)
	}
	return SettingsState{
		Presets:            presetList(),
		PresetID:           c.PresetID,
		AppliedPresetID:    c.AppliedPresetID,
		AppliedCustomImage: c.AppliedCustomImage,
		OverlayOn:          overlayOn,
		CustomImage:        c.CustomImage,
		CustomName:  name,
		AllMonitors: c.AllMonitors,
		Version:     appVersion,
	}
}

func thumbDataURIfromFile(path string) string {
	img, err := loadCustomImage(path)
	if err != nil {
		return ""
	}
	c := coverImage(img, 444, 284)
	buf := new(bytes.Buffer)
	if err := jpeg.Encode(buf, c, &jpeg.Options{Quality: 82}); err != nil {
		return ""
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// openSettingsWindow 打开设置面板。
// 遮罩期间打开：不撤遮罩，改为把面板置顶并暂停遮罩的抢顶定时器
// （面板压在遮罩之上可见，屏保不中断；远控端仍看不到遮罩）。
func openSettingsWindow() {

	// 重新打开面板时丢弃"未确认的选择"：选中态回到生效态，
	// 高亮应落在"正在使用/上次使用"的那张图上（点选未确认不算数）
	c0 := getConfig()
	if (c0.AppliedPresetID != "" || c0.AppliedCustomImage != "") &&
		(c0.PresetID != c0.AppliedPresetID || c0.CustomImage != c0.AppliedCustomImage) {
		updateConfig(func(cc *Config) {
			cc.PresetID = c0.AppliedPresetID
			cc.CustomImage = c0.AppliedCustomImage
		})
		logf("INFO: 打开面板：未确认的选择已丢弃，选中回到生效配置 (%s)", c0.AppliedPresetID)
	}

	if settingsWV != nil {
		w := settingsWV
		// Dispatch 投递到 webview 线程：显示 + 置前 + 刷新状态
		w.Dispatch(func() {
			showSettingsWindow(w)
			w.Eval("boot()") // 状态可能已变化，让前端重新拉取
		})
		settingsVisible = true
		logf("INFO: 设置窗口已显示（复用）")
		return
	}

	// 首次创建：放到独立线程并常驻 Run（go-webview2 的 Bind/RPC 依赖 Run 的消息泵，
	// 不 Run 则前端 window.goState 等绑定根本不会被注入）
	go createSettingsThread()
}

// createSettingsThread 创建 WebView2 实例并常驻消息泵。
// 子类化拦截 WM_CLOSE，窗口永不销毁 → Run 永不退出 → 不会 PostQuitMessage 击穿主循环。
func createSettingsThread() {
	runtime.LockOSThread()

	dataPath := filepath.Join(os.Getenv("LOCALAPPDATA"), "Veil", "WebView2")
	_ = os.MkdirAll(dataPath, 0755)

	// 按当前 DPI 放大窗口，保证 CSS 视口正好是设计时的 560×470 逻辑像素
	dpi, _, _ := procGetDpiForSystem.Call()
	scale := float64(dpi) / 96.0
	if scale < 1 {
		scale = 1
	}

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:    false,
		DataPath: dataPath,
		WindowOptions: webview2.WindowOptions{
			Title:  "Veil · 设置隐私屏保",
			Width:  uint(560 * scale),
			Height: uint(470 * scale),
			Center: true,
			IconId: uint(appIconResID()),
		},
	})
	if w == nil {
		logf("ERROR: 创建 WebView2 窗口失败（可能缺少 WebView2 运行时）")
		return
	}

	bindSettings(w)
	w.SetHtml(settingsHTML)

	// 子类化设置窗：拦截 WM_CLOSE（否则库会销毁窗口并击穿主消息循环）
	hwnd := w.Window()
	settingsProcCb = syscall.NewCallback(settingsSubclassedProc)
	const gwlpWndproc = uintptr(0xFFFFFFFFFFFFFFFC) // GWLP_WNDPROC = -4
	settingsOrigWndProc, _, _ = procSetWindowLongPtrW2.Call(
		uintptr(hwnd), gwlpWndproc, settingsProcCb)

	settingsMu.Lock()
	settingsWV = w
	settingsVisible = true
	settingsMu.Unlock()
	logf("INFO: 设置窗口已创建")

	w.Run() // 常驻消息泵（Bind/RPC 依赖它）

	// 只有在窗口被销毁时才会到这里（正常流程已被子类化拦截）
	settingsMu.Lock()
	settingsWV = nil
	settingsVisible = false
	settingsMu.Unlock()
	logf("INFO: 设置窗口线程结束")
}

// showSettingsWindow 显示面板。
// 遮罩期间面板**保持普通层级**（在遮罩下方）：物理屏被遮罩挡住看不到面板，
// 远控端因遮罩被捕获排除而能看到并操作面板——隐私与可操作性兼得。
func showSettingsWindow(w webview2.WebView) {
	hwnd := w.Window()
	procShowWindow.Call(uintptr(hwnd), uintptr(SW_SHOWNOACTIVATE))
	user32SetForeground(hwnd)
}

// bindSettings 注册前端可调用的 Go 方法。回调都在 UI 线程执行，可直接同步操作。
func bindSettings(w webview2.WebView) {
	_ = w.Bind("goState", func() string {
		b, _ := json.Marshal(buildState())
		return string(b)
	})

	// 缩略图按需加载：单次消息只带一张（goState 一次性回传全部 base64
	// 会超出 WebView2 binding 的可靠回传体积，导致前端拿到空预设）
	_ = w.Bind("goThumb", func(id string) string {
		return thumbFor(id)
	})

	_ = w.Bind("goCustomThumb", func() string {
		c := getConfig()
		if c.CustomImage == "" {
			return ""
		}
		return thumbDataURIfromFile(c.CustomImage)
	})

	_ = w.Bind("goPickImage", func() string {
		p := showOpenImageDialog()
		if p == "" {
			return ""
		}
		if msg := validateImageFile(p); msg != "" {
			b, _ := json.Marshal(map[string]string{"error": msg})
			return string(b)
		}
		updateConfig(func(c *Config) {
			c.CustomImage = p
		})
		dw, dh := imageDims(p)
		b, _ := json.Marshal(map[string]interface{}{
			"path":  p,
			"name":  filepath.Base(p),
			"dims":  fmt.Sprintf("%d×%d", dw, dh),
			"low":   dw < recWidth || dh < recHeight,
		})
		return string(b)
	})

	_ = w.Bind("goSetPreset", func(id string) string {
		updateConfig(func(c *Config) {
			c.PresetID = id
			c.CustomImage = ""
		})
		return "ok"
	})

	_ = w.Bind("goClearCustom", func() string {
		updateConfig(func(c *Config) {
			c.CustomImage = ""
		})
		return "ok"
	})

	_ = w.Bind("goSetAllMonitors", func(v bool) string {
		updateConfig(func(c *Config) {
			c.AllMonitors = v
		})
		return "ok"
	})

	_ = w.Bind("goPreview", func() string {
		enablePreview()
		return "ok"
	})

	// 确定并启用：隐藏设置窗并开启遮罩（无论之前是否开启）
	_ = w.Bind("goApplyEnable", func() string {
		logf("INFO: 用户确认并启用防窥")
		hideSettings()
		postCmd(cmdApply) // 快照生效态 + 重建/开启遮罩，均在 UI 线程
		return "ok"
	})

	_ = w.Bind("goClose", func() string {
		hideSettings()
		return "ok"
	})

	_ = w.Bind("goGetHotkeys", func() string {
		return hotkeyStatusJSON()
	})

	// 热键只能在 UI 线程注册 / 注销，而绑定函数跑在 WebView 线程：
	// 这几个函数内部经 runOnUI 转过去（见 hotkey.go）
	_ = w.Bind("goSetHotkey", setHotkeyFromPanel)
	_ = w.Bind("goSetHotkeyOff", setHotkeyOffFromPanel)
	_ = w.Bind("goSuggestHotkeys", suggestHotkeysFromPanel)
}

// hideSettings 隐藏设置窗；面板若被临时置顶则恢复正常层级，并恢复遮罩抢顶。
func hideSettings() {
	settingsMu.Lock()
	w := settingsWV
	settingsMu.Unlock()
	if w == nil {
		return
	}

	w.Dispatch(func() {
		procShowWindow.Call(uintptr(w.Window()), SW_HIDE)
	})
	settingsVisible = false
	logf("INFO: 设置窗口已隐藏")
}

// enablePreview 在物理屏上试放 2.5 秒。
// 遮罩只能由 UI 线程创建/销毁，这里投递消息而不是直接调用。
func enablePreview() {
	postCmd(cmdPreview)
}

// ---------- 图片校验 ----------

const (
	maxImageBytes = 2 * 1024 * 1024
	// minWidth/minHeight 不再是硬限制：低于该分辨率只提示"放大后会略模糊"，
	// 毕竟遮罩是 cover 缩放，用户自己的图自己负责
	recWidth  = 1920
	recHeight = 1080
)

// imageDims 读取图片尺寸。
func imageDims(path string) (int, int) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	if cfg, _, err := image.DecodeConfig(f); err == nil {
		return cfg.Width, cfg.Height
	}
	return 0, 0
}

// validateImageFile 返回空串表示通过，否则返回给用户看的错误文案。
// 只拦截格式与体积；分辨率放行为软提示（前端负责）。
func validateImageFile(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return "无法读取该文件"
	}
	if st.Size() > maxImageBytes {
		return fmt.Sprintf("图片体积 %.1fMB 超过 2MB 限制", float64(st.Size())/1024/1024)
	}
	f, err := os.Open(path)
	if err != nil {
		return "无法打开该文件"
	}
	defer f.Close()

	_, format, err := image.DecodeConfig(f)
	if err != nil {
		return "无法识别的图片格式，仅支持 JPG / PNG / WEBP"
	}
	if format != "jpeg" && format != "png" && format != "webp" {
		return "仅支持 JPG / PNG / WEBP 格式"
	}
	return ""
}

// ---------- 系统文件选择对话框 ----------

type openFileNameW struct {
	StructSize        uint32
	HwndOwner         windows.Handle
	HInstance         windows.Handle
	LpstrFilter       *uint16
	LpstrCustomFilter *uint16
	NMaxCustFilter    uint32
	NFilterIndex      uint32
	LpstrFile         *uint16
	NMaxFile          uint32
	LpstrFileTitle    *uint16
	NMaxFileTitle     uint32
	LpstrInitialDir   *uint16
	LpstrTitle        *uint16
	Flags             uint32
	NFileOffset       uint16
	NFileExtension    uint16
	LpstrDefExt       *uint16
	LCustData         uintptr
	LpfnHook          uintptr
	LpTemplateName    *uint16
	PvReserved        unsafe.Pointer
	DwReserved        uint32
	FlagsEx           uint32
}

const (
	OFN_FILEMUSTEXIST = 0x00001000
	OFN_PATHMUSTEXIST = 0x00000800
	OFN_HIDEREADONLY  = 0x00000004
	OFN_EXPLORER      = 0x00080000
)

func showOpenImageDialog() string {
	comdlg32 := windows.NewLazySystemDLL("comdlg32.dll")
	procGetOpenFileNameW := comdlg32.NewProc("GetOpenFileNameW")

	buf := make([]uint16, 1024)
	filter := "图片文件 (*.jpg;*.jpeg;*.png;*.webp)\x00*.jpg;*.jpeg;*.png;*.webp\x00所有文件 (*.*)\x00*.*\x00\x00"
	filterUTF16, _ := syscall.UTF16FromString(filter)
	filterUTF16 = append(filterUTF16, 0) // 双 null 结尾

	titleUTF16, _ := syscall.UTF16FromString("选择隐私屏保图片")

	var ofn openFileNameW
	ofn.StructSize = uint32(unsafe.Sizeof(ofn))
	ofn.HwndOwner = controlHwnd
	ofn.LpstrFilter = &filterUTF16[0]
	ofn.NFilterIndex = 1
	ofn.LpstrFile = &buf[0]
	ofn.NMaxFile = uint32(len(buf))
	ofn.LpstrTitle = &titleUTF16[0]
	ofn.Flags = OFN_FILEMUSTEXIST | OFN_PATHMUSTEXIST | OFN_HIDEREADONLY | OFN_EXPLORER
	ofn.LpstrDefExt, _ = syscall.UTF16PtrFromString("png")

	r, _, _ := procGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}


// user32SetForeground 把窗口带到前台（显示面板时必须，否则只在后台）。
func user32SetForeground(hwnd unsafe.Pointer) {
	user32SetForegroundWindow.Call(uintptr(hwnd))
}

var user32SetForegroundWindow = windows.NewLazySystemDLL("user32.dll").NewProc("SetForegroundWindow")
