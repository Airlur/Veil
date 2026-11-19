// hotkey.go — 可自定义的全局热键系统。
//
// 三组热键：
//   id 1  开关遮罩（可自定义，默认 Ctrl+Alt+V）
//   id 2  退出程序（可自定义，默认 Ctrl+Alt+Shift+Q）
//   id 3  兜底强制退出（固定 Ctrl+Alt+Shift+Q，永不注销、不参与自定义）
//
// 兜底键的意义：无论用户把热键改成什么、甚至改成一组无效组合，
// Ctrl+Alt+Shift+Q 永远能把程序退出来——遮罩盖着屏幕时这是唯一的保险丝。
package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

const (
	idHotkeyToggle = 1
	idHotkeyQuit     = 2
	idHotkeyEscape   = 3
	idHotkeySettings = 4
)

// MOD_WIN 缺省常量补齐
const MOD_WIN = 0x0008

var (
	hkToggleMods, hkToggleVK uint32
	hkQuitMods, hkQuitVK     uint32
	hkMu                     sync.Mutex
)

// activeHotkeyDesc 当前生效的开关热键，供托盘菜单、Toast、日志文案使用。
var activeHotkeyDesc = defaultHotkeyToggle

type hotkeyReq struct {
	kind   string
	combo  string
	result chan string
}

var (
	pendingHK *hotkeyReq
)

// parseHotkey 解析 "Ctrl+Alt+V" 形式的组合键 → (修饰键位, 主键 VK 码)。
func parseHotkey(s string) (uint32, uint32, error) {
	mods := uint32(0)
	vk := uint32(0)
	parts := strings.Split(s, "+")
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("组合键至少需要 Ctrl 或 Alt 加一个主键，例如 Ctrl+Alt+V")
	}
	for _, raw := range parts {
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
		// 主键
		if vk != 0 {
			return 0, 0, fmt.Errorf("只能有一个主键")
		}
		if len(p) == 1 {
			c := p[0]
			if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
				vk = uint32(c)
				continue
			}
			return 0, 0, fmt.Errorf("主键仅支持字母 / 数字 / F1~F12")
		}
		if len(p) >= 2 && p[0] == 'F' {
			n := 0
			fmt.Sscanf(p[1:], "%d", &n)
			if n >= 1 && n <= 12 {
				vk = uint32(0x70 + n - 1)
				continue
			}
			return 0, 0, fmt.Errorf("功能键仅支持 F1~F12")
		}
		return 0, 0, fmt.Errorf("不支持的主键: %s", p)
	}
	if vk == 0 {
		return 0, 0, fmt.Errorf("缺少主键（字母 / 数字 / F1~F12）")
	}
	if mods&(MOD_CONTROL|MOD_ALT) == 0 {
		return 0, 0, fmt.Errorf("必须包含 Ctrl 或 Alt，否则会干扰正常打字")
	}
	return mods, vk, nil
}

// registerOne 注册热键。必须在 UI 线程调用：WM_HOTKEY 会投递到注册线程，
// 只有控制窗口线程在跑消息循环，热键才有人响应。
func registerOne(id, mods, vk uint32) bool {
	r, _, _ := procRegisterHotKey.Call(uintptr(controlHwnd), uintptr(id), uintptr(mods), uintptr(vk))
	return r != 0
}

func unregisterOne(id uint32) {
	procUnregisterHotKey.Call(uintptr(controlHwnd), uintptr(id))
}

// buildHotkeys 按配置注册全部热键（UI 线程调用）。
// 单个热键注册失败自动退回默认组合；兜底键失败只在日志里警告。
func buildHotkeys() {
	cfg := getConfig()

	// 兜底：固定组合，永不舍弃
	if m, v, err := parseHotkey(fallbackHotkey); err == nil {
		if registerOne(idHotkeyEscape, m, v) {
			logf("INFO: 兜底退出键 = %s", fallbackHotkey)
		} else {
			logf("WARNING: 兜底退出键 %s 注册失败", fallbackHotkey)
		}
	}

	registerFromConfig(idHotkeyToggle, cfg.HotkeyToggle, defaultHotkeyToggle)
	registerFromConfig(idHotkeyQuit, cfg.HotkeyQuit, defaultHotkeyQuit)
	registerFromConfig(idHotkeySettings, cfg.HotkeySettings, defaultHotkeySettings)
}

func registerFromConfig(id uint32, combo, fallback string) {
	// 退出热键与兜底键组合相同时：兜底键（ID 3）已承担退出行为，
	// 同一个组合无法注册两次，跳过即可——功能完全一致，不视为失败。
	if id == idHotkeyQuit && strings.EqualFold(combo, fallbackHotkey) {
		cacheHotkey(id, combo)
		return
	}
	m, v, err := parseHotkey(combo)
	if err != nil || !registerOne(id, m, v) {
		// 退回默认组合
		if m2, v2, err2 := parseHotkey(fallback); err2 == nil {
			registerOne(id, m2, v2)
			combo = fallback
			if err != nil {
				logf("WARNING: 热键 %s 解析失败(%v)，已退回 %s", combo, err, fallback)
			} else {
				logf("WARNING: 热键 %s 注册失败，已退回 %s", combo, fallback)
			}
		}
	}
	cacheHotkey(id, combo)
}

func cacheHotkey(id uint32, combo string) {
	m, v, err := parseHotkey(combo)
	if err != nil {
		return
	}
	hkMu.Lock()
	if id == idHotkeyToggle {
		hkToggleMods, hkToggleVK = m, v
	} else if id == idHotkeyQuit {
		hkQuitMods, hkQuitVK = m, v
	}
	hkMu.Unlock()
	if id == idHotkeyToggle {
		activeHotkeyDesc = combo
	}
}

// applySetHotkey 修改热键（UI 线程调用，由 cmdSetHotkey 消息驱动）。
// 流程：校验 → 注销旧 → 注册新 → 失败回滚 → 写配置。
// 返回 JSON 字符串给前端。
func applySetHotkey(kind, combo string) string {
	if kind != "toggle" && kind != "quit" && kind != "settings" {
		return jsonError("未知的热键类型")
	}
	if combo == fallbackHotkey {
		return jsonError("该组合已保留给强制退出，请换一个")
	}
	mods, vk, err := parseHotkey(combo)
	if err != nil {
		return jsonError(err.Error())
	}

	cfg := getConfig()
	var id uint32
	var other, old string
	if kind == "toggle" {
		id = uint32(idHotkeyToggle)
		other = cfg.HotkeyToggle
		old = cfg.HotkeyToggle
	} else if kind == "settings" {
		id = uint32(idHotkeySettings)
		other = cfg.HotkeySettings
		old = cfg.HotkeySettings
	} else {
		id = uint32(idHotkeyQuit)
		other = cfg.HotkeyQuit
		old = cfg.HotkeyQuit
	}
	if strings.EqualFold(combo, other) {
		return jsonError("该组合已分配给另一个热键")
	}
	// 与兜底组合相同时仅"开关"禁止——兜底键承担退出，因此"退出"热键设为兜底组合是允许的
	if kind == "toggle" {
		if fm, fv, ferr := parseHotkey(fallbackHotkey); ferr == nil && fm == mods && fv == vk {
			return jsonError("该组合已保留给强制退出，请换一个")
		}
	}

	unregisterOne(id)
	// 退出热键设为兜底组合时不再重复注册（兜底键 ID 3 已承担），跳过即可
	skipRegister := kind == "quit" && strings.EqualFold(combo, fallbackHotkey)
	if !skipRegister {
		if !registerOne(id, mods, vk) {
			// 回滚
			if om, ov, oerr := parseHotkey(old); oerr == nil {
				registerOne(id, om, ov)
			}
			return jsonError(fmt.Sprintf("组合 %s 已被其他程序占用，已保留原热键", combo))
		}
	}

	updateConfig(func(c *Config) {
		if kind == "toggle" {
			c.HotkeyToggle = combo
		} else if kind == "settings" {
			c.HotkeySettings = combo
		} else {
			c.HotkeyQuit = combo
		}
	})
	cacheHotkey(id, combo)
	updateTrayTip()
	logf("INFO: 热键[%s] 已改为 %s", kind, combo)
	// 统一返回 JSON：前端靠它区分成败并触发刷新（纯字符串会被 JSON.parse 拒绝）
	b, _ := json.Marshal(map[string]string{"ok": combo})
	return string(b)
}

func jsonError(msg string) string {
	return fmt.Sprintf(`{"error":%q}`, msg)
}
