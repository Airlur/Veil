package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestHotkeyRules(t *testing.T) {
	cases := []struct {
		in string
		ok bool
	}{
		{"Ctrl+Alt+V", true},
		{"Ctrl+Alt+Shift+Q", true},
		{"Alt+F5", true},
		{"Win+Shift+K", true},
		{"Ctrl+Alt+Left", true},
		{"Ctrl+Alt+Num5", true},
		{"Ctrl+Alt+`", true},
		{"Shift+F9", true},        // F1~F12 搭配 Shift 即可
		{"V", false},              // 会吞掉正常打字
		{"Shift+A", false},        // 同上：打不出大写字母
		{"Shift+Left", false},     // 会吞掉选中文本
		{"F5", false},             // F1~F12 不能单独用
		{"Ctrl", false},           // 缺主键
		{"Ctrl+Alt+Shift", false}, // 缺主键
		{"Ctrl+Alt+F25", false},   // 不存在的键
		{"Ctrl+Alt+A+B", false},   // 两个主键
		{"Ctrl+C", false},         // 复制
		{"Alt+F4", false},         // 关闭窗口
	}
	for _, c := range cases {
		_, _, err := parseHotkey(c.in)
		if (err == nil) != c.ok {
			t.Errorf("parseHotkey(%q) err=%v, 期望 ok=%v", c.in, err, c.ok)
		}
	}
}

// 按键表里的每个键都要能 格式化 → 解析 往返，否则写进配置后下次启动读不回来。
func TestHotkeyFormatRoundTrip(t *testing.T) {
	for vk := range hotkeyNameByVK {
		for _, mods := range []uint32{MOD_CONTROL | MOD_ALT, MOD_CONTROL | MOD_ALT | MOD_SHIFT | MOD_WIN} {
			s := formatHotkey(mods, vk)
			m, v, err := parseHotkey(s)
			if err != nil || m != mods || v != vk {
				t.Errorf("%q 往返失败: mods=%#x vk=%#x err=%v", s, m, v, err)
			}
		}
	}
}

// startHotkeyTestUI 启动一个真实的 UI 线程（控制窗口 + 消息循环），与程序运行时的线程模型一致。
func startHotkeyTestUI(t *testing.T) {
	t.Helper()
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		uiThreadID = curThreadID()
		controlWndProcCallback = syscall.NewCallback(controlWndProc)
		if err := createControlWindow(); err != nil {
			ready <- err
			return
		}
		ready <- nil
		var msg MSG
		for {
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if int32(r) <= 0 {
				return
			}
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
		}
	}()
	if err := <-ready; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		procPostMessageW.Call(uintptr(controlHwnd), WM_CLOSE, 0, 0) // 销毁窗口会一并注销它的全部热键
		time.Sleep(150 * time.Millisecond)
		controlHwnd, uiThreadID = 0, 0
		hkMu.Lock()
		hkRegistered = map[uint32]bool{}
		hkMu.Unlock()
	})
}

// 设置面板改热键的完整路径。测试 goroutine 扮演 WebView 线程（不是 UI 线程），
// 调用的就是绑定给前端的那几个函数。会短暂注册真实的全局热键（Ctrl+Alt+Shift+1~6）。
func TestHotkeyFromPanelThread(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	startHotkeyTestUI(t)
	if curThreadID() == uiThreadID {
		t.Fatal("测试线程与 UI 线程相同，测不出线程问题")
	}

	const cas = MOD_CONTROL | MOD_ALT | MOD_SHIFT
	f := func(n uint32) uint32 { return 0x60 + n } // Ctrl+Alt+Shift+Num1~6：小键盘数字几乎不会有程序占用
	cfg = defaultConfig()
	cfg.HotkeyToggle = formatHotkey(cas, f(2))
	cfg.HotkeySettings = formatHotkey(cas, f(3))
	cfg.HotkeyQuit = formatHotkey(cas, f(4))
	cfgPath = filepath.Join(t.TempDir(), "config.json")

	// probe 在 UI 线程试注册一个组合：返回它当前是否空闲
	probe := func(mods, vk uint32) bool {
		r, ok := runOnUI(func() string {
			free, _ := registerOne(idHotkeyProbe, mods, vk)
			if free {
				unregisterOne(idHotkeyProbe)
				return "free"
			}
			return "taken"
		})
		if !ok {
			t.Fatal("runOnUI 没有执行")
		}
		return r == "free"
	}
	status := func() map[string]struct {
		Combo            string
		Active, Disabled bool
	} {
		var m map[string]struct {
			Combo            string
			Active, Disabled bool
		}
		if err := json.Unmarshal([]byte(hotkeyStatusJSON()), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	result := func(raw string) hotkeyResult {
		var r hotkeyResult
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			t.Fatalf("返回不是合法 JSON: %q", raw)
		}
		return r
	}

	for n := uint32(1); n <= 6; n++ {
		if !probe(cas, f(n)) {
			t.Skipf("本机 %s 不可用（被占用或环境不支持注册热键），跳过", formatHotkey(cas, f(n)))
		}
	}
	runOnUI(func() string { buildHotkeys(); return "" })
	st := status()
	if !st["toggle"].Active || !st["settings"].Active || !st["quit"].Active {
		t.Fatalf("启动注册后应全部生效: %+v", st)
	}

	// 1. 从面板线程改热键必须成功（修复前：任何组合都报"已被其他程序占用"）
	r := result(setHotkeyFromPanel("toggle", cas, int(f(6))))
	if r.Error != "" || r.OK != formatHotkey(cas, f(6)) {
		t.Fatalf("从面板线程修改失败: %+v", r)
	}
	if probe(cas, f(6)) {
		t.Error("新组合没有真的注册上")
	}
	if !probe(cas, f(2)) {
		t.Error("旧组合没有注销")
	}
	if got := getConfig().HotkeyToggle; got != formatHotkey(cas, f(6)) {
		t.Errorf("配置未更新: %q", got)
	}
	if data, _ := os.ReadFile(cfgPath); !strings.Contains(string(data), "Num6") {
		t.Errorf("配置文件未写入新组合: %s", data)
	}

	// 2. 真的被占用：报占用、原热键保留、给出的建议确实空闲
	runOnUI(func() string { registerOne(77, cas, f(5)); return "" }) // 扮演"别的程序"
	r = result(setHotkeyFromPanel("settings", cas, int(f(5))))
	if r.Error == "" || !r.Occupied {
		t.Fatalf("应报告被占用: %+v", r)
	}
	if probe(cas, f(3)) {
		t.Error("修改失败后原热键丢了（必须回滚）")
	}
	if !status()["settings"].Active || getConfig().HotkeySettings != formatHotkey(cas, f(3)) {
		t.Error("修改失败后状态 / 配置不应变化")
	}
	if len(r.Suggest) == 0 {
		t.Error("被占用时应给出空闲组合")
	}
	for _, s := range r.Suggest {
		if !probe(s.Mods, s.VK) {
			t.Errorf("建议的 %s 实际不空闲", s.Combo)
		}
		if m, v, err := parseHotkey(s.Combo); err != nil || m != s.Mods || v != s.VK {
			t.Errorf("建议的 %s 与其 mods/vk 不一致", s.Combo)
		}
	}
	t.Logf("被占用时给出的建议: %+v", r.Suggest)

	// 3. 与另一组热键相同：明确指出是哪一组，而不是说"被其他程序占用"
	r = result(setHotkeyFromPanel("settings", cas, int(f(6))))
	if !strings.Contains(r.Error, "开关防窥") {
		t.Errorf("与开关热键相同时的提示不对: %+v", r)
	}

	// 4. 规则：Shift+字母拒绝，兜底组合只能给"退出"
	if r = result(setHotkeyFromPanel("toggle", MOD_SHIFT, 'A')); r.Error == "" {
		t.Error("Shift+A 应被拒绝")
	}
	em, ev, _ := parseHotkey(fallbackHotkey)
	if r = result(setHotkeyFromPanel("toggle", int(em), int(ev))); r.Error == "" {
		t.Error("兜底组合不应允许给开关热键")
	}

	// 5. 禁用 / 启用：只注销、组合键保留；"重启"后保持；启用恢复原组合
	r = result(setHotkeyOffFromPanel("toggle", true))
	if r.Error != "" {
		t.Fatalf("禁用失败: %+v", r)
	}
	if !probe(cas, f(6)) {
		t.Error("禁用后组合仍被注册")
	}
	if s := status()["toggle"]; !s.Disabled || s.Active || s.Combo != formatHotkey(cas, f(6)) || !getConfig().HotkeyToggleOff {
		t.Errorf("禁用后状态不对: %+v", s)
	}
	runOnUI(func() string { buildHotkeys(); return "" })
	if s := status()["toggle"]; !s.Disabled || s.Active || !probe(cas, f(6)) {
		t.Errorf("重新注册后禁用状态丢失: %+v", s)
	}
	r = result(setHotkeyOffFromPanel("toggle", false))
	if r.Error != "" || probe(cas, f(6)) || !status()["toggle"].Active {
		t.Errorf("启用后应恢复原组合并生效: %+v %+v", r, status()["toggle"])
	}

	// 托盘"禁用快捷键"：一键全部禁用 / 恢复，与面板读取的是同一份状态
	runOnUI(func() string { applySetAllHotkeysOff(true); return "" })
	st = status()
	if !allHotkeysOff() || !st["toggle"].Disabled || !st["settings"].Disabled || !st["quit"].Disabled {
		t.Errorf("一键禁用后状态不对: %+v", st)
	}
	if !probe(cas, f(6)) || !probe(cas, f(3)) || !probe(cas, f(4)) {
		t.Error("一键禁用后仍有热键被注册")
	}
	runOnUI(func() string { applySetAllHotkeysOff(false); return "" })
	st = status()
	if allHotkeysOff() || !st["toggle"].Active || !st["settings"].Active || !st["quit"].Active {
		t.Errorf("一键恢复后应全部生效: %+v", st)
	}

	// 给已禁用的热键录制新组合 = 同时启用
	result(setHotkeyOffFromPanel("toggle", true))
	r = result(setHotkeyFromPanel("toggle", cas, int(f(1))))
	if r.Error != "" || !status()["toggle"].Active || status()["toggle"].Disabled {
		t.Errorf("禁用后重新设置失败: %+v %+v", r, status()["toggle"])
	}

	// 6. 退出热键设为兜底组合：允许，由兜底键承担
	runOnUI(func() string {
		ok, _ := registerOne(idHotkeyEscape, em, ev)
		setRegistered(idHotkeyEscape, ok)
		return ""
	})
	if !isRegistered(idHotkeyEscape) {
		t.Skip("兜底组合在本机被占用（多半是正在运行的 Veil），跳过最后一项")
	}
	r = result(setHotkeyFromPanel("quit", int(em), int(ev)))
	if r.Error != "" || !status()["quit"].Active {
		t.Errorf("退出热键设为兜底组合应成功且生效: %+v %+v", r, status()["quit"])
	}
	if !probe(cas, f(4)) {
		t.Error("退出热键的旧组合没有注销")
	}
}
