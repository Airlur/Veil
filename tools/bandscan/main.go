// bandscan — 列出所有顶层窗口的 Z 序 band，用于验证 Veil 遮罩是否压在
// 输入法候选窗 / 右键菜单之上。输出按实际 Z 序自顶向下排列（Z 列越小越靠上），
// 以行的先后为准——band 的数值大小不代表上下关系。
//
// 用法：go run ./tools/bandscan            列出 band != 1 的窗口 + Veil 遮罩
//       go run ./tools/bandscan -all       列出全部可见窗口
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procFindWindowExW            = user32.NewProc("FindWindowExW")
	procGetWindowBand            = user32.NewProc("GetWindowBand")
	procGetClassNameW            = user32.NewProc("GetClassNameW")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
)

func processName(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return fmt.Sprintf("pid %d", pid)
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 520)
	n := uint32(len(buf))
	if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) != nil {
		return fmt.Sprintf("pid %d", pid)
	}
	return filepath.Base(syscall.UTF16ToString(buf[:n]))
}

func main() {
	all := len(os.Args) > 1 && os.Args[1] == "-all"
	fmt.Printf("%-5s %-4s %-10s %-26s %-40s %s\n", "Z", "band", "hwnd", "process", "class", "title")

	// FindWindowEx 按 Z 序自顶向下遍历，且能看到 EnumWindows 跳过的高 band 窗口
	var hwnd uintptr
	for z := 0; ; z++ {
		hwnd, _, _ = procFindWindowExW.Call(0, hwnd, 0, 0)
		if hwnd == 0 {
			break
		}
		var band uint32
		procGetWindowBand.Call(hwnd, uintptr(unsafe.Pointer(&band)))
		vis, _, _ := procIsWindowVisible.Call(hwnd)

		cls := make([]uint16, 256)
		procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&cls[0])), 256)
		class := syscall.UTF16ToString(cls)
		isVeil := class == "VeilOverlayWindow"

		if !isVeil && !all && band == 1 {
			continue
		}
		if !isVeil && all && vis == 0 {
			continue
		}

		title := make([]uint16, 256)
		procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&title[0])), 256)
		var pid uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))

		mark := " "
		if vis != 0 {
			mark = "*" // 可见
		}
		fmt.Printf("%-5d %-4d %#-10x %-26.26s %-40.40s %s%s\n", z, band, hwnd, processName(pid), class, mark, syscall.UTF16ToString(title))
	}
}
