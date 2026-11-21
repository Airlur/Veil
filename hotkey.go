// hotkey.go — 可自定义的全局热键系统。
//
// 四组热键：
//
//	id 1  开关遮罩（可自定义）
//	id 2  退出程序（可自定义）
//	id 4  打开设置（可自定义）
//	id 3  兜底强制退出（固定 Ctrl+Alt+Shift+Q，永不注销、不参与自定义）
//
// 兜底键的意义：无论用户把热键改成什么、禁用了哪些，
// Ctrl+Alt+Shift+Q 永远能把程序退出来——遮罩盖着屏幕时这是唯一的保险丝。
// 三组可自定义热键都可以单独禁用（配置里的 hotkey*Off），组合键保留，启用时恢复。
//
// 线程：RegisterHotKey / UnregisterHotKey 只能在控制窗口所属的 UI 线程调用，
// 其他线程调用会以 1408 失败（曾被误报成"已被其他程序占用"，导致任何组合都改不了）。
// 设置面板的绑定函数跑在 WebView 线程，必须经 runOnUI 转到 UI 线程。
package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"syscall"
)

const (
	idHotkeyToggle   = 1
	idHotkeyQuit     = 2
	idHotkeyEscape   = 3
	idHotkeySettings = 4
	// idHotkeyProbe 探测某个组合是否空闲时临时占用
	idHotkeyProbe = 99
)

// MOD_WIN 缺省常量补齐
const MOD_WIN = 0x0008

// errHotkeyAlreadyRegistered 是 RegisterHotKey 在组合已被（任何进程）注册时的错误码。
const errHotkeyAlreadyRegistered = 1409

// hotkeyKind 描述一组可自定义热键。
type hotkeyKind struct {
	name  string // 前端使用的标识：toggle / settings / quit
	label string // 界面文案
	id    uint32
	def   string
	get   func(*Config) string
	set   func(*Config, string)
	off   func(*Config) *bool // 该组的"已禁用"开关
}

var hotkeyKinds = []hotkeyKind{
	{"toggle", "开关防窥", idHotkeyToggle, defaultHotkeyToggle,
		func(c *Config) string { return c.HotkeyToggle }, func(c *Config, s string) { c.HotkeyToggle = s },
		func(c *Config) *bool { return &c.HotkeyToggleOff }},
	{"settings", "打开设置", idHotkeySettings, defaultHotkeySettings,
		func(c *Config) string { return c.HotkeySettings }, func(c *Config, s string) { c.HotkeySettings = s },
		func(c *Config) *bool { return &c.HotkeySettingsOff }},
	{"quit", "退出程序", idHotkeyQuit, defaultHotkeyQuit,
		func(c *Config) string { return c.HotkeyQuit }, func(c *Config, s string) { c.HotkeyQuit = s },
		func(c *Config) *bool { return &c.HotkeyQuitOff }},
}

func findHotkeyKind(name string) *hotkeyKind {
	for i := range hotkeyKinds {
		if hotkeyKinds[i].name == name {
			return &hotkeyKinds[i]
		}
	}
	return nil
}

var (
	hkMu sync.Mutex
	// hkRegistered 记录每个 id 当前是否真的注册成功（只在 UI 线程写）。
	hkRegistered = map[uint32]bool{}
)

func setRegistered(id uint32, ok bool) {
	hkMu.Lock()
	hkRegistered[id] = ok
	hkMu.Unlock()
}

func isRegistered(id uint32) bool {
	hkMu.Lock()
	defer hkMu.Unlock()
	return hkRegistered[id]
}

// hotkeyActive 返回某组热键当前是否真的能触发。
// 退出热键与兜底组合相同时不单独注册，由兜底键承担。
func hotkeyActive(k *hotkeyKind, c *Config) bool {
	if *k.off(c) {
		return false
	}
	if k.id == idHotkeyQuit && strings.EqualFold(k.get(c), fallbackHotkey) {
		return isRegistered(idHotkeyEscape)
	}
	return isRegistered(k.id)
}

// toggleHotkeyState 返回开关热键、是否生效、是否被用户禁用，供托盘提示、启动提示条、日志使用。
func toggleHotkeyState() (combo string, active, disabled bool) {
	c := getConfig()
	k := findHotkeyKind("toggle")
	combo = k.get(&c)
	return combo, hotkeyActive(k, &c), *k.off(&c)
}

// ---------- 按键表 ----------

var (
	hotkeyVKByName = map[string]uint32{} // 名称（大写）→ VK
	hotkeyNameByVK = map[uint32]string{} // VK → 显示名称
)

func init() {
	add := func(name string, vk uint32) {
		hotkeyVKByName[strings.ToUpper(name)] = vk
		hotkeyNameByVK[vk] = name
	}
	for c := uint32('A'); c <= 'Z'; c++ {
		add(string(rune(c)), c)
	}
	for c := uint32('0'); c <= '9'; c++ {
		add(string(rune(c)), c)
		add(fmt.Sprintf("Num%c", rune(c)), 0x60+c-'0') // 小键盘数字
	}
	for n := uint32(1); n <= 12; n++ {
		add(fmt.Sprintf("F%d", n), 0x70+n-1)
	}
	for name, vk := range map[string]uint32{
		"Space": 0x20, "PageUp": 0x21, "PageDown": 0x22, "End": 0x23, "Home": 0x24,
		"Left": 0x25, "Up": 0x26, "Right": 0x27, "Down": 0x28, "Insert": 0x2D, "Delete": 0x2E,
		// 标点按美式键盘命名；其他键盘布局下名称可能与键帽不符，但注册用的是 VK，功能不受影响
		";": 0xBA, "=": 0xBB, ",": 0xBC, "-": 0xBD, ".": 0xBE, "/": 0xBF, "`": 0xC0,
		"[": 0xDB, "\\": 0xDC, "]": 0xDD, "'": 0xDE,
	} {
		add(name, vk)
	}
}

// reservedHotkeys 是占用后会破坏日常操作的组合，一律拒绝。
var reservedHotkeys = map[[2]uint32]bool{
	{MOD_CONTROL, 'C'}: true, {MOD_CONTROL, 'V'}: true, {MOD_CONTROL, 'X'}: true,
	{MOD_CONTROL, 'Z'}: true, {MOD_CONTROL, 'Y'}: true, {MOD_CONTROL, 'A'}: true,
	{MOD_CONTROL, 'S'}: true,
	{MOD_ALT, 0x73}:    true, // Alt+F4
}

// validateHotkey 检查组合是否允许作为全局热键。规则只拦"会破坏正常使用"的组合：
//   - F1~F12：至少一个修饰键（Shift 也行）
//   - 其余按键（字母、数字、标点、方向键等）：必须含 Ctrl、Alt 或 Win，
//     否则打字、选中文本时就会被全局热键吞掉
func validateHotkey(mods, vk uint32) error {
	if _, ok := hotkeyNameByVK[vk]; !ok {
		return fmt.Errorf("不支持这个按键")
	}
	switch {
	case vk >= 0x70 && vk <= 0x7B: // F1~F12
		if mods == 0 {
			return fmt.Errorf("F1~F12 需要搭配至少一个修饰键（Ctrl / Alt / Shift / Win）")
		}
	default:
		if mods&(MOD_CONTROL|MOD_ALT|MOD_WIN) == 0 {
			return fmt.Errorf("这个按键必须搭配 Ctrl、Alt 或 Win，否则会影响正常打字")
		}
	}
	if reservedHotkeys[[2]uint32{mods, vk}] {
		return fmt.Errorf("%s 是常用的系统 / 编辑快捷键，占用后会影响正常使用", formatHotkey(mods, vk))
	}
	return nil
}

// formatHotkey 生成规范的组合键文本，例如 "Ctrl+Alt+V"。
func formatHotkey(mods, vk uint32) string {
	var parts []string
	if mods&MOD_CONTROL != 0 {
		parts = append(parts, "Ctrl")
	}
	if mods&MOD_ALT != 0 {
		parts = append(parts, "Alt")
	}
	if mods&MOD_SHIFT != 0 {
		parts = append(parts, "Shift")
	}
	if mods&MOD_WIN != 0 {
		parts = append(parts, "Win")
	}
	return strings.Join(append(parts, hotkeyNameByVK[vk]), "+")
}

// parseHotkey 解析 "Ctrl+Alt+V" 形式的组合键 → (修饰键位, 主键 VK 码)，并做合法性检查。
func parseHotkey(s string) (uint32, uint32, error) {
	mods := uint32(0)
	vk := uint32(0)
	for _, raw := range strings.Split(s, "+") {
		p := strings.ToUpper(strings.TrimSpace(raw))
		switch p {
		case "CTRL":
			mods |= MOD_CONTROL
			continue
		case "ALT":
			mods |= MOD_ALT
			continue
		case "SHIFT":
			mods |= MOD_SHIFT
			continue
		case "WIN":
			mods |= MOD_WIN
			continue
		}
		if vk != 0 {
			return 0, 0, fmt.Errorf("只能有一个主键")
		}
		v, ok := hotkeyVKByName[p]
		if !ok {
			return 0, 0, fmt.Errorf("不支持的按键: %s", raw)
		}
		vk = v
	}
	if vk == 0 {
		return 0, 0, fmt.Errorf("缺少主键")
	}
	if err := validateHotkey(mods, vk); err != nil {
		return 0, 0, err
	}
	return mods, vk, nil
}

func isEscapeCombo(mods, vk uint32) bool {
	m, v, err := parseHotkey(fallbackHotkey)
	return err == nil && m == mods && v == vk
}

// ---------- 注册 ----------

// registerOne 注册热键，返回是否成功及失败时的系统错误码。必须在 UI 线程调用：
// WM_HOTKEY 会投递到注册线程，只有控制窗口线程在跑消息循环，热键才有人响应。
func registerOne(id, mods, vk uint32) (bool, uint32) {
	// MOD_NOREPEAT：按住不放不会连续触发（否则遮罩会反复开关）
	r, _, err := procRegisterHotKey.Call(uintptr(controlHwnd), uintptr(id), uintptr(mods|MOD_NOREPEAT), uintptr(vk))
	if r != 0 {
		return true, 0
	}
	errno, _ := err.(syscall.Errno)
	return false, uint32(errno)
}

func unregisterOne(id uint32) {
	procUnregisterHotKey.Call(uintptr(controlHwnd), uintptr(id))
}

// hotkeyFailText 把注册失败的错误码翻译成给用户看的原因。
func hotkeyFailText(combo string, code uint32) string {
	if code == errHotkeyAlreadyRegistered {
		return fmt.Sprintf("%s 已被其他程序占用", combo)
	}
	return fmt.Sprintf("%s 注册失败（系统错误 %d）", combo, code)
}

// buildHotkeys 按配置注册全部热键（UI 线程调用）。
// 注册失败的热键保持"未生效"并如实显示在面板上，不会悄悄换成别的组合。
func buildHotkeys() {
	// 先注销再注册：重复调用（重新加载配置）时不会因为"自己已经占着"而失败
	if m, v, err := parseHotkey(fallbackHotkey); err == nil {
		unregisterOne(idHotkeyEscape)
		ok, code := registerOne(idHotkeyEscape, m, v)
		setRegistered(idHotkeyEscape, ok)
		if ok {
			logf("INFO: 兜底退出键 = %s", fallbackHotkey)
		} else {
			logf("WARNING: 兜底退出键未生效：%s", hotkeyFailText(fallbackHotkey, code))
		}
	}

	for i := range hotkeyKinds {
		k := &hotkeyKinds[i]
		unregisterOne(k.id)
		setRegistered(k.id, false)
		c := getConfig()
		combo := k.get(&c)
		m, v, err := parseHotkey(combo)
		if err != nil {
			// 配置里的组合无效（多为手工编辑）：改回默认值
			logf("WARNING: 热键[%s] %q 无效(%v)，已改回默认 %s", k.name, combo, err, k.def)
			combo = k.def
			updateConfig(func(cc *Config) { k.set(cc, combo) })
			m, v, _ = parseHotkey(combo)
		}
		if *k.off(&c) {
			setRegistered(k.id, false)
			logf("INFO: 热键[%s] 已禁用", k.name)
			continue
		}
		// 退出热键与兜底组合相同：兜底键已承担退出，同一组合无法注册两次
		if k.id == idHotkeyQuit && isEscapeCombo(m, v) {
			continue
		}
		ok, code := registerOne(k.id, m, v)
		setRegistered(k.id, ok)
		if !ok {
			logf("WARNING: 热键[%s] 未生效：%s", k.name, hotkeyFailText(combo, code))
		}
	}
}

// ---------- 修改（设置面板） ----------

type hotkeySuggestion struct {
	Combo string `json:"combo"`
	Mods  uint32 `json:"mods"`
	VK    uint32 `json:"vk"`
}

type hotkeyResult struct {
	OK       string             `json:"ok,omitempty"`
	Error    string             `json:"error,omitempty"`
	Occupied bool               `json:"occupied,omitempty"`
	Suggest  []hotkeySuggestion `json:"suggest,omitempty"`
}

func (r hotkeyResult) json() string {
	b, _ := json.Marshal(r)
	return string(b)
}

// setHotkeyFromPanel 是设置面板的 goSetHotkey 绑定。
// 绑定函数跑在 WebView 线程，而热键只能在 UI 线程注册 / 注销，必须经 runOnUI 转过去。
// mods 为 MOD_* 位，vk 为虚拟键码（前端的 keyCode）。
func setHotkeyFromPanel(kind string, mods, vk int) string {
	r, ok := runOnUI(func() string { return applySetHotkey(kind, uint32(mods), uint32(vk)) })
	if !ok {
		return jsonError("程序主线程没有响应，请稍后再试")
	}
	return r
}

// setHotkeyOffFromPanel 是 goSetHotkeyOff 绑定：禁用 / 启用一组热键。
func setHotkeyOffFromPanel(kind string, off bool) string {
	r, ok := runOnUI(func() string { return applySetHotkeyOff(kind, off) })
	if !ok {
		return jsonError("程序主线程没有响应，请稍后再试")
	}
	return r
}

// suggestHotkeysFromPanel 是 goSuggestHotkeys 绑定：返回几个当前空闲的组合。
func suggestHotkeysFromPanel() string {
	r, ok := runOnUI(func() string {
		b, _ := json.Marshal(suggestFreeHotkeys(4))
		return string(b)
	})
	if !ok {
		return "[]"
	}
	return r
}

// applySetHotkey 修改热键。必须在 UI 线程调用。
// 流程：校验 → 注销旧 → 注册新 → 失败回滚 → 写配置。返回 JSON 给前端。
func applySetHotkey(kind string, mods, vk uint32) string {
	k := findHotkeyKind(kind)
	if k == nil {
		return jsonError("未知的热键类型")
	}
	if err := validateHotkey(mods, vk); err != nil {
		return jsonError(err.Error())
	}
	combo := formatHotkey(mods, vk)
	escape := isEscapeCombo(mods, vk)
	// 兜底组合只允许给"退出"用（兜底键本来就是退出）
	if escape && k.id != idHotkeyQuit {
		return jsonError("该组合已保留给强制退出，请换一个")
	}
	c := getConfig()
	for i := range hotkeyKinds {
		o := &hotkeyKinds[i]
		if o.id == k.id {
			continue
		}
		if om, ov, err := parseHotkey(o.get(&c)); err == nil && om == mods && ov == vk {
			return jsonError(fmt.Sprintf("%s 已用于「%s」", combo, o.label))
		}
	}

	old := k.get(&c)
	if strings.EqualFold(old, combo) && hotkeyActive(k, &c) {
		return hotkeyResult{OK: combo}.json() // 没有变化
	}

	wasRegistered := isRegistered(k.id)
	if wasRegistered {
		unregisterOne(k.id)
		setRegistered(k.id, false)
	}
	if !escape {
		ok, code := registerOne(k.id, mods, vk)
		if !ok {
			// 回滚：原热键原样恢复，绝不出现"热键没了程序失联"
			if om, ov, err := parseHotkey(old); err == nil && wasRegistered {
				back, _ := registerOne(k.id, om, ov)
				setRegistered(k.id, back)
			}
			logf("INFO: 热键[%s] 改为 %s 失败（错误 %d），保留 %s", k.name, combo, code, old)
			res := hotkeyResult{Error: hotkeyFailText(combo, code) + "，原热键不变"}
			if code == errHotkeyAlreadyRegistered {
				res.Occupied = true
				res.Suggest = suggestFreeHotkeys(4)
			}
			return res.json()
		}
		setRegistered(k.id, true)
	}

	// 给已禁用的热键设置新组合 = 同时启用它
	updateConfig(func(cc *Config) { k.set(cc, combo); *k.off(cc) = false })
	updateTrayTip()
	logf("INFO: 热键[%s] 已改为 %s", k.name, combo)
	return hotkeyResult{OK: combo}.json()
}

// setHotkeyOff 禁用或启用一组热键。必须在 UI 线程调用。
// 禁用只注销、不动组合键；启用时按原组合重新注册，返回是否生效及失败的错误码。
func setHotkeyOff(k *hotkeyKind, off bool) (bool, uint32) {
	updateConfig(func(cc *Config) { *k.off(cc) = off })
	if isRegistered(k.id) {
		unregisterOne(k.id)
		setRegistered(k.id, false)
	}
	if off {
		logf("INFO: 热键[%s] 已禁用", k.name)
		return true, 0
	}
	c := getConfig()
	m, v, err := parseHotkey(k.get(&c))
	if err != nil {
		return false, 0
	}
	if k.id == idHotkeyQuit && isEscapeCombo(m, v) {
		return isRegistered(idHotkeyEscape), errHotkeyAlreadyRegistered
	}
	ok, code := registerOne(k.id, m, v)
	setRegistered(k.id, ok)
	logf("INFO: 热键[%s] 已启用 %s（生效=%v）", k.name, k.get(&c), ok)
	return ok, code
}

// applySetHotkeyOff 是设置面板"禁用 / 启用"按钮的实现。必须在 UI 线程调用。
// 兜底键不在 hotkeyKinds 里，无法被禁用——三组全禁用后仍能用它或托盘菜单退出。
func applySetHotkeyOff(kind string, off bool) string {
	k := findHotkeyKind(kind)
	if k == nil {
		return jsonError("未知的热键类型")
	}
	ok, code := setHotkeyOff(k, off)
	updateTrayTip()
	c := getConfig()
	if !ok {
		res := hotkeyResult{Error: hotkeyFailText(k.get(&c), code) + "，暂未生效"}
		if code == errHotkeyAlreadyRegistered {
			res.Occupied = true
			res.Suggest = suggestFreeHotkeys(4)
		}
		return res.json()
	}
	return hotkeyResult{OK: k.get(&c)}.json()
}

// allHotkeysOff 返回三组热键是否全部处于禁用状态（托盘菜单的勾选状态）。
func allHotkeysOff() bool {
	c := getConfig()
	for i := range hotkeyKinds {
		if !*hotkeyKinds[i].off(&c) {
			return false
		}
	}
	return true
}

// applySetAllHotkeysOff 是托盘"禁用快捷键"的实现：一键禁用 / 恢复全部三组。必须在 UI 线程调用。
func applySetAllHotkeysOff(off bool) {
	for i := range hotkeyKinds {
		setHotkeyOff(&hotkeyKinds[i], off)
	}
	updateTrayTip()
	notifyHotkeysChanged()
}

// notifyHotkeysChanged 让设置面板重新读取热键状态，托盘里的改动才能和面板保持同步。
func notifyHotkeysChanged() {
	settingsMu.Lock()
	w := settingsWV
	visible := settingsVisible
	settingsMu.Unlock()
	if w == nil || !visible {
		return
	}
	w.Dispatch(func() {
		w.Eval("loadHotkeys()")
	})
}

// suggestFreeHotkeys 实际探测并返回 n 个当前没有被任何程序注册的组合。必须在 UI 线程调用。
// Windows 不提供"某个热键被谁占用"的查询，能做的只有替用户把空闲的找出来。
// 注意：只能发现通过 RegisterHotKey 注册的占用；用键盘钩子实现快捷键的程序探测不到。
func suggestFreeHotkeys(n int) []hotkeySuggestion {
	var out []hotkeySuggestion
	try := func(mods, vk uint32) bool {
		if validateHotkey(mods, vk) != nil || isEscapeCombo(mods, vk) {
			return false
		}
		ok, _ := registerOne(idHotkeyProbe, mods, vk)
		if !ok {
			return false // 被别的程序或 Veil 自己的其他热键占着
		}
		unregisterOne(idHotkeyProbe)
		out = append(out, hotkeySuggestion{formatHotkey(mods, vk), mods, vk})
		return len(out) >= n
	}
	// 每种样式最多取 2 个，让建议覆盖几种不同的按法
	groups := []struct {
		mods uint32
		keys []uint32
	}{
		{MOD_CONTROL | MOD_ALT, []uint32{'V', 'B', 'H', 'M', 'K', 'J', 'U', 'Y', 'G', 'N', 'P', 'L', 'O', 'I', 'T', 'R', 'E', 'W', 'D', 'F', 'Q', 'Z'}},
		{MOD_CONTROL | MOD_SHIFT, fKeys(1, 12)},
		{MOD_ALT | MOD_SHIFT, fKeys(1, 12)},
		{MOD_CONTROL | MOD_ALT | MOD_SHIFT, []uint32{'V', 'B', 'H', 'M', 'K', 'J', 'U', 'Y', 'G', 'N', 'P', 'L'}},
		{MOD_CONTROL | MOD_ALT, fKeys(1, 12)},
	}
	for _, g := range groups {
		got := 0
		for _, vk := range g.keys {
			before := len(out)
			done := try(g.mods, vk)
			if done {
				return out
			}
			if len(out) > before {
				if got++; got >= 2 {
					break
				}
			}
		}
	}
	return out
}

func fKeys(from, to uint32) []uint32 {
	var ks []uint32
	for n := from; n <= to; n++ {
		ks = append(ks, 0x70+n-1)
	}
	return ks
}

// hotkeyStatusJSON 返回各热键的组合与是否生效，供设置面板显示。任意线程可调用。
func hotkeyStatusJSON() string {
	type st struct {
		Combo    string `json:"combo"`
		Active   bool   `json:"active"`
		Disabled bool   `json:"disabled"`
	}
	c := getConfig()
	out := map[string]st{"escape": {fallbackHotkey, isRegistered(idHotkeyEscape), false}}
	for i := range hotkeyKinds {
		k := &hotkeyKinds[i]
		combo := k.get(&c)
		out[k.name] = st{combo, hotkeyActive(k, &c), *k.off(&c)}
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func jsonError(msg string) string {
	return hotkeyResult{Error: msg}.json()
}
