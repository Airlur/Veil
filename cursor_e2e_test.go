package main

import (
	"fmt"
	"os"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 这些测试会真的替换系统光标（约 1 秒），默认跳过：
//   set VEIL_CURSOR_E2E=1 && go test -count=1 -run CursorE2E -v .

// arrowState 返回系统箭头光标当前的宽度和可见像素数（其他程序读到的就是这个）。
// 分别画在黑底和白底上：任何一处偏离底色的像素即为可见。
func arrowState() (width int32, visible int) {
	h, _, _ := procLoadCursorW.Call(0, 32512)
	var ii ICONINFO
	if r, _, _ := procGetIconInfo.Call(h, uintptr(unsafe.Pointer(&ii))); r == 0 {
		return 0, -1
	}
	var bm BITMAP
	procGetObjectW.Call(uintptr(ii.HbmMask), unsafe.Sizeof(bm), uintptr(unsafe.Pointer(&bm)))
	procDeleteObject.Call(uintptr(ii.HbmMask))
	if ii.HbmColor != 0 {
		procDeleteObject.Call(uintptr(ii.HbmColor))
	}
	w := bm.BmWidth

	draw := func(fill byte) []byte {
		var bi BITMAPINFO
		bi.BmiHeader.BiSize = uint32(unsafe.Sizeof(BITMAPINFOHEADER{}))
		bi.BmiHeader.BiWidth = w
		bi.BmiHeader.BiHeight = -w
		bi.BmiHeader.BiPlanes = 1
		bi.BmiHeader.BiBitCount = 32
		var bits unsafe.Pointer
		bmp, _, _ := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bi)), DIB_RGB_COLORS, uintptr(unsafe.Pointer(&bits)), 0, 0)
		dc, _, _ := procCreateCompatibleDC.Call(0)
		defer procDeleteObject.Call(bmp)
		defer procDeleteDC.Call(dc)
		procSelectObject.Call(dc, bmp)
		pix := unsafe.Slice((*byte)(bits), int(w)*int(w)*4)
		for i := range pix {
			pix[i] = fill
		}
		procDrawIconEx.Call(dc, 0, 0, h, uintptr(w), uintptr(w), 0, 0, DI_NORMAL)
		return append([]byte(nil), pix...)
	}
	black, white := draw(0x00), draw(0xFF)
	for i := 0; i < len(black); i += 4 {
		if black[i+1] != 0x00 || white[i+1] != 0xFF {
			visible++
		}
	}
	return w, visible
}

func TestCursorE2E(t *testing.T) {
	if os.Getenv("VEIL_CURSOR_E2E") == "" {
		t.Skip("需要 VEIL_CURSOR_E2E=1")
	}
	defer restoreSystemCursors() // 无论如何都恢复
	cfg = defaultConfig()
	watchdogStarted = true // 测试进程不拉看门狗
	w0, before := arrowState()
	if before <= 0 {
		t.Fatalf("测试前箭头光标不可见(%d)，系统光标状态异常", before)
	}

	hideCursors()
	w1, n := arrowState()
	t.Logf("箭头光标：隐藏前 %dpx / %d 可见像素，隐藏后 %dpx / %d 可见像素", w0, before, w1, n)
	if n != 0 {
		t.Errorf("隐藏后箭头光标仍有 %d 个可见像素", n)
	}
	// 透明光标必须远小于真实光标，UU 才会改从光标文件加载（见 cursor.go 文件头）
	if w1*4 > w0 {
		t.Errorf("透明光标尺寸 %d 不够小（真实光标 %d）", w1, w0)
	}

	showCursors()
	if w2, after := arrowState(); after <= 0 || w2 != w0 {
		t.Errorf("恢复后箭头光标异常：%dpx / %d 可见像素", w2, after)
	}
}

// 模拟崩溃：隐藏光标后强杀自己，由看门狗恢复。该用例以进程被杀（FAIL）结束属预期，
// 随后跑 TestCursorE2EAfterCrash 判定。VEIL_WATCHDOG_EXE 需指向非 UIAccess 的 Veil.exe。
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
	if _, n := arrowState(); n != 0 {
		restoreSystemCursors()
		t.Fatalf("隐藏失败 %d", n)
	}
	fmt.Println("HIDDEN_OK: cursor hidden, killing self")
	windows.TerminateProcess(windows.CurrentProcess(), 1)
}

func TestCursorE2EAfterCrash(t *testing.T) {
	if os.Getenv("VEIL_CURSOR_E2E") == "" {
		t.Skip("需要 VEIL_CURSOR_E2E=1")
	}
	if _, n := arrowState(); n <= 0 {
		restoreSystemCursors()
		t.Fatalf("看门狗没有恢复光标（箭头可见像素=%d），已手动恢复", n)
	}
}
