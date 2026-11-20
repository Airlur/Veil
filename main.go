// main.go — 程序入口：单实例、DPI 声明、UI 线程启动、消息循环。
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	appName    = "Veil"
	appVersion = "1.0.0"
	mutexName  = `Global\VeilSingleInstance_7f3a`
)

var logFile *os.File

func main() {
	// 看门狗子进程：只负责等主进程结束后恢复光标，不走后面的任何初始化
	if len(os.Args) > 2 && os.Args[1] == watchdogFlag {
		runCursorWatchdog(os.Args[2])
		return
	}
	// 上次若异常退出，系统光标可能还是透明的：无条件恢复一次
	restoreSystemCursors()

	// 必须在任何窗口创建之前声明 DPI 感知，否则 200% 缩放下遮罩只盖住 1/4 屏
	setProcessDpiAwareness()
	initLog()
	defer func() {
		if logFile != nil {
			logFile.Close()
		}
	}()

	logf("INFO: %s v%s 启动", appName, appVersion)

	if !acquireSingleInstance() {
		// 已有实例：通知其退出（顺带关闭遮罩），然后由本实例接管运行（重启语义）。
		// 屏幕被遮住时双击 exe 依然是有效的自救方式：遮罩会关闭、程序换新。
		logf("INFO: 检测到已有实例，通知其退出并由本实例接管")
		notifyExistingInstance()
		acquired := false
		for i := 0; i < 30; i++ {
			time.Sleep(100 * time.Millisecond)
			if acquireSingleInstance() {
				acquired = true
				break
			}
		}
		if !acquired {
			logf("ERROR: 旧实例 3 秒内未退出，本次启动放弃")
			return
		}
		logf("INFO: 已接管，继续启动")
	}

	loadConfig()
	logf("INFO: 数据目录 %s", dataDir())
	initTrayIcon()

	// -probe：自检模式。开启遮罩 3 秒后自动退出，用于在命令行确认底层 API 是否生效。
	// -settings：打开设置窗口，几秒后自动隐藏退出（调试用）。
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-probe":
			probeMode = true
		case "-settings":
			openSettingsWindow()
			time.Sleep(6 * time.Second) // 给 WebView2 留出创建时间
			hideSettings()
			return
		}
	}

	ready := make(chan struct{})
	go uiThread(ready)

	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		logf("ERROR: UI 线程初始化超时")
		return
	}

	// UI 线程持有消息循环，主 goroutine 在此等待进程结束
	<-quitCh
	logf("INFO: 程序退出")
}

var (
	quitCh    = make(chan struct{})
	probeMode bool
)

// uiThread 负责所有窗口的创建与消息循环。
// 必须 LockOSThread：Win32 窗口与线程绑定，Go 调度器换线程会导致消息投递到错误的线程。
func uiThread(ready chan struct{}) {
	runtime.LockOSThread()
	uiThreadID = curThreadID()
	logf("INFO: UI 线程 ID = %d", uiThreadID)

	overlayWndProcCallback = syscall.NewCallback(overlayWndProc)
	controlWndProcCallback = syscall.NewCallback(controlWndProc)
	toastWndProcCb = syscall.NewCallback(toastWndProc)

	if err := createControlWindow(); err != nil {
		logf("ERROR: 创建控制窗口失败: %v", err)
		close(ready) // 失败路径也要放行，避免主线程空等 5 秒
		return
	}

	buildHotkeys()

	if err := addTrayIcon(); err != nil {
		logf("WARNING: 托盘图标添加失败: %v", err)
	}
	// 自绘提示条：托盘图标在 Win11 里默认折叠进 "^"，只有声音和气泡都不可靠
	toggleCombo, toggleActive, toggleOff := toggleHotkeyState()
	switch {
	case toggleActive:
		showToast("Veil 已启动", fmt.Sprintf("%s 开启防窥 · 右键托盘图标设置屏保", toggleCombo))
	case toggleOff:
		showToast("Veil 已启动", "开关热键已禁用 · 右键托盘图标开启防窥")
	default:
		// 热键被占用时必须明说，否则用户以为按了没反应是程序坏了
		showToast("Veil 已启动", fmt.Sprintf("开关热键 %s 被占用未生效 · 请在设置面板更换", toggleCombo))
	}

	updateTrayTip()

	logf("INFO: 就绪（开关热键 %s，生效=%v；Ctrl+Alt+Shift+Q 退出）", toggleCombo, toggleActive)

	// 初始化到此结束：必须先放行主线程，再进入阻塞的消息循环。
	// 写成 defer close(ready) 会让主线程一直等到消息循环退出（即进程结束）才继续，属于死锁。
	close(ready)

	// 声音反馈：程序没有主窗口，用户需要知道它确实起来了
	messageBeep()
	// 启动即打开设置面板：走消息队列到 UI 线程执行
	postCmd(cmdOpenSettings)

	if probeMode {
		logf("INFO: 自检模式 — 开启遮罩 3 秒后自动退出")
		enableOverlay()
		procSetTimer.Call(uintptr(controlHwnd), TIMER_PROBE, 3000, 0)
	}

	messageLoop()
}

func messageLoop() {
	var msg MSG
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
	close(quitCh)
}

// ---------- 单实例 ----------

const errorAlreadyExists = 183

func acquireSingleInstance() bool {
	name, _ := syscall.UTF16PtrFromString(mutexName)
	h, _, err := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return true // 连互斥体都建不出来，不阻塞用户使用
	}
	// CreateMutexW 在"已存在"时仍返回有效句柄，靠 GetLastError=183 区分
	if e, ok := err.(syscall.Errno); ok && uint32(e) == errorAlreadyExists {
		return false
	}
	return true
}

func notifyExistingInstance() {
	hwnd, _, _ := procFindWindowW.Call(
		uintptr(unsafe.Pointer(utf16Ptr(controlClassName))),
		0,
	)
	if hwnd != 0 {
		procPostMessageW.Call(hwnd, uintptr(cmdQuit), 0, 0)
	}
}

// ---------- 日志 ----------

// logFilePath 返回日志文件路径（托盘"查看日志"与 initLog 共用）。
// 便携版写 %TEMP%（不在 exe 目录留文件）；安装版和配置放在一起（%APPDATA%\Veil）。
func logFilePath() string {
	if d := dataDir(); d != exeDir() {
		return filepath.Join(d, "Veil.log")
	}
	return filepath.Join(os.TempDir(), "Veil.log")
}

func initLog() {
	path := logFilePath()
	// 超过 1MB 时轮换，避免无限增长
	if st, err := os.Stat(path); err == nil && st.Size() > 1<<20 {
		_ = os.Rename(path, path+".old")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		logFile = f
	}
}

func logf(format string, args ...interface{}) {
	if logFile == nil {
		return
	}
	ts := time.Now().Format("2006-01-02 15:04:05.000")
	fmt.Fprintf(logFile, "[%s] %s\n", ts, fmt.Sprintf(format, args...))
	_ = logFile.Sync()
}

// 确保 windows 包被引用（部分文件按平台裁剪时仍需）
var _ = windows.Handle(0)
