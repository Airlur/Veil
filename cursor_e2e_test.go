package main

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 这些测试会真的替换系统光标（约 1 秒），默认跳过：
//   set VEIL_CURSOR_E2E=1 && go test -run CursorE2E -v .

func arrowOpaquePixels() int {
	h, _, _ := procLoadCursorW.Call(0, 32512)
	s := renderCursorShape(h)
	if s == nil {
		return -1
	}
	defer procDeleteDC.Call(s.dc)
	defer procDeleteObject.Call(s.bmp)
	n := 0
	for i := 3; i < len(s.pix); i += 4 {
		if s.pix[i] != 0 {
			n++
		}
	}
	return n
}

func TestCursorE2E(t *testing.T) {
	if os.Getenv("VEIL_CURSOR_E2E") == "" {
		t.Skip("需要 VEIL_CURSOR_E2E=1")
	}
	defer restoreSystemCursors() // 无论如何都恢复
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	cfg = defaultConfig()
	cfg.RemoteCursor = true
	// 替身光标只在有遮罩时才显示：建一块真实遮罩（物理屏会被盖住约 1 秒）
	overlayWndProcCallback = syscall.NewCallback(overlayWndProc)
	if err := registerOverlayClass(); err != nil {
		t.Fatal(err)
	}
	var prim Monitor
	for _, m := range enumMonitors() {
		if m.Primary {
			prim = m
		}
	}
	ow, err := createOneOverlay(prim)
	if err != nil {
		t.Fatal(err)
	}
	activeOverlays = []*overlayWindow{ow}
	defer func() {
		procDestroyWindow.Call(uintptr(ow.hwnd))
		ow.releaseBitmap()
		activeOverlays = nil
	}()
	watchdogStarted = true // 测试进程不拉看门狗
	before := arrowOpaquePixels()
	if before <= 0 {
		t.Fatalf("测试前箭头光标不可见(%d)，系统光标状态异常", before)
	}

	hideCursors()
	if n := arrowOpaquePixels(); n != 0 {
		t.Errorf("隐藏后箭头光标仍有 %d 个可见像素", n)
	}
	if cursorHwnd == 0 {
		t.Fatal("替身光标窗口未创建")
	}
	var ci CURSORINFO
	ci.CbSize = uint32(unsafe.Sizeof(ci))
	procGetCursorInfo.Call(uintptr(unsafe.Pointer(&ci)))
	shape := cursorShapes[uintptr(ci.HCursor)]
	t.Logf("当前光标句柄 %#x showing=%v 命中形状表=%v 表大小=%d", ci.HCursor, ci.Flags&CURSOR_SHOWING != 0, shape != nil, len(cursorShapes))
	syncCursorWindow()
	vis, _, _ := user32.NewProc("IsWindowVisible").Call(uintptr(cursorHwnd))
	var rc RECT
	procGetWindowRect.Call(uintptr(cursorHwnd), uintptr(unsafe.Pointer(&rc)))
	var aff uint32
	user32.NewProc("GetWindowDisplayAffinity").Call(uintptr(cursorHwnd), uintptr(unsafe.Pointer(&aff)))
	t.Logf("替身窗口 visible=%v rect=%+v 光标位置=%+v band=%d affinity=%#x", vis != 0, rc, ci.PtScreenPos, windowBand(cursorHwnd), aff)
	if shape != nil && ci.Flags&CURSOR_SHOWING != 0 {
		if vis == 0 {
			t.Error("光标可见且是系统光标，但替身窗口未显示")
		}
		if rc.Left+shape.hotX != ci.PtScreenPos.X || rc.Top+shape.hotY != ci.PtScreenPos.Y {
			t.Error("替身窗口位置与光标热点不一致")
		}
	}
	if aff != 0 {
		t.Error("替身窗口不应设置捕获排除")
	}
	// Z 序：反复抢顶 + 同步，替身必须始终紧贴在遮罩正下方，一次都不能跑到遮罩上面
	getWindow := user32.NewProc("GetWindow")
	for i := 0; i < 40; i++ {
		keepOnTop()
		syncCursorWindow()
		above, _, _ := getWindow.Call(uintptr(cursorHwnd), 3) // GW_HWNDPREV
		if windows.Handle(above) != ow.hwnd {
			t.Fatalf("第 %d 轮：替身光标上方的窗口是 %#x，不是遮罩 %#x", i, above, ow.hwnd)
		}
		time.Sleep(10 * time.Millisecond)
	}

	showCursors()
	// 只要求恢复可见：系统重载光标后尺寸可能随 DPI 与测试前不同
	if after := arrowOpaquePixels(); after <= 0 {
		t.Errorf("恢复后箭头光标不可见(%d)", after)
	}
	if cursorHwnd != 0 || len(cursorShapes) != 0 {
		t.Error("showCursors 未清理干净")
	}
}

// 模拟崩溃：隐藏光标后直接退出进程，由看门狗恢复。
// 需要 VEIL_WATCHDOG_EXE 指向已构建的 Veil.exe。
func TestCursorE2ECrash(t *testing.T) {
	exe := os.Getenv("VEIL_WATCHDOG_EXE")
	if os.Getenv("VEIL_CURSOR_E2E") == "" || exe == "" {
		t.Skip("需要 VEIL_CURSOR_E2E=1 和 VEIL_WATCHDOG_EXE")
	}
	cfg = defaultConfig()
	watchdogStarted = true
	args := fmt.Sprintf("%s %d", watchdogFlag, os.Getpid())
	if err := windows.ShellExecute(0, utf16Ptr("open"), utf16Ptr(exe), utf16Ptr(args), nil, windows.SW_HIDE); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond) // 等看门狗拿到本进程句柄
	hideCursors()
	if n := arrowOpaquePixels(); n != 0 {
		restoreSystemCursors()
		t.Fatalf("隐藏失败 %d", n)
	}
	fmt.Println("HIDDEN_OK: cursor hidden, killing self")
	// 不恢复，直接强杀自己（等同任务管理器结束进程）
	windows.TerminateProcess(windows.CurrentProcess(), 1)
}

func TestCursorE2EAfterCrash(t *testing.T) {
	if os.Getenv("VEIL_CURSOR_E2E") == "" {
		t.Skip("需要 VEIL_CURSOR_E2E=1")
	}
	if n := arrowOpaquePixels(); n <= 0 {
		restoreSystemCursors()
		t.Fatalf("看门狗没有恢复光标（箭头可见像素=%d），已手动恢复", n)
	}
}
