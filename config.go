// config.go — 配置持久化（config.json，不写注册表）。
// 便携版存放于程序同目录；安装版（Program Files 不可写）存放于 %APPDATA%\Veil。
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Config 是全部可持久化的用户设置。
type Config struct {
	// PresetID 为预设图标识，例如 "black" / "slate" / "carbon"
	PresetID string `json:"presetId"`
	// CustomImage 为用户上传图片的绝对路径；非空时优先于 PresetID
	CustomImage string `json:"customImage"`
	// AllMonitors 为 true 时遮住所有显示器，false 只遮主屏
	AllMonitors bool `json:"allMonitors"`
	// AppliedPresetID / AppliedCustomImage 是当前**实际生效**（遮罩正在使用）的配置。
	// PresetID / CustomImage 表示"选中待应用"。两者分离，才能区分
	// "我点了这张图"和"屏保正在用这张图"。
	AppliedPresetID    string `json:"appliedPresetId"`
	AppliedCustomImage string `json:"appliedCustomImage"`
	// HotkeyToggle 开关遮罩的热键，格式 "Ctrl+Alt+V"
	HotkeyToggle string `json:"hotkeyToggle"`
	// HotkeyQuit 退出程序的热键
	HotkeyQuit string `json:"hotkeyQuit"`
	// HotkeySettings 打开设置面板的热键
	HotkeySettings string `json:"hotkeySettings"`
	// 三组热键各自的"已禁用"开关。组合键本身保留，重新启用时恢复原组合
	HotkeyToggleOff   bool `json:"hotkeyToggleOff,omitempty"`
	HotkeySettingsOff bool `json:"hotkeySettingsOff,omitempty"`
	HotkeyQuitOff     bool `json:"hotkeyQuitOff,omitempty"`
	// HideCursor 遮罩期间隐藏物理屏鼠标光标。指针类型：配置里没有这个字段时按 true 处理
	HideCursorOpt *bool `json:"hideCursor,omitempty"`
	HideCursor    bool  `json:"-"` // 解析后的生效值
}

// 默认热键。兜底键 fallbackHotkey 永远固定，不参与自定义。
const (
	defaultHotkeyToggle   = "Ctrl+Alt+X"
	defaultHotkeyQuit     = "Ctrl+Alt+C"
	defaultHotkeySettings = "Ctrl+Alt+S"
	// fallbackHotkey 强制逃生键：无论用户把热键改成什么，这个组合永远能退出程序。
	fallbackHotkey = "Ctrl+Alt+Shift+Q"
)

var (
	cfg     Config
	cfgMu   sync.RWMutex
	cfgPath string
)

func defaultConfig() Config {
	return Config{
		PresetID:       "black",
		CustomImage:    "",
		AllMonitors:    true,
		HotkeyToggle:   defaultHotkeyToggle,
		HotkeyQuit:     defaultHotkeyQuit,
		HotkeySettings: defaultHotkeySettings,
		HideCursor:     true,
	}
}

// exeDir 返回 exe 所在目录（动态预设目录、配置文件的基准）。
func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

var (
	dataDirOnce sync.Once
	dataDirPath string
)

// dataDir 返回用户数据目录（config.json、presets\）。
// exe 同目录可写时就用它（便携版行为不变）；不可写（安装在 Program Files 的
// UIAccess 版）时退到 %APPDATA%\Veil。
func dataDir() string {
	dataDirOnce.Do(func() {
		dataDirPath = exeDir()
		probe := filepath.Join(dataDirPath, ".veil-write-test")
		if f, err := os.Create(probe); err == nil {
			f.Close()
			os.Remove(probe)
			return
		}
		if base, err := os.UserConfigDir(); err == nil {
			dir := filepath.Join(base, appName)
			if os.MkdirAll(filepath.Join(dir, "presets"), 0755) == nil {
				dataDirPath = dir
			}
		}
	})
	return dataDirPath
}

// presetsDir 返回目录预设所在文件夹。
func presetsDir() string {
	return filepath.Join(dataDir(), "presets")
}

// configFilePath 返回配置文件路径。
func configFilePath() string {
	return filepath.Join(dataDir(), "config.json")
}

func loadConfig() {
	cfgPath = configFilePath()
	cfg = defaultConfig()

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return // 首次运行，用默认值
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return
	}
	if c.PresetID == "" && c.CustomImage == "" {
		c.PresetID = "black"
	}
	if c.HotkeyToggle == "" {
		c.HotkeyToggle = defaultHotkeyToggle
	}
	if c.HotkeyQuit == "" {
		c.HotkeyQuit = defaultHotkeyQuit
	}
	if c.HotkeySettings == "" {
		c.HotkeySettings = defaultHotkeySettings
	}
	// 老配置没有 applied 字段：视为"选中即生效"，避免升级后徽章全部消失
	if c.AppliedPresetID == "" && c.AppliedCustomImage == "" {
		c.AppliedPresetID = c.PresetID
		c.AppliedCustomImage = c.CustomImage
	}
	c.HideCursor = c.HideCursorOpt == nil || *c.HideCursorOpt
	cfgMu.Lock()
	cfg = c
	cfgMu.Unlock()
}

func saveConfig() error {
	cfgMu.RLock()
	c := cfg
	cfgMu.RUnlock()

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cfgPath, data, 0644)
}

func getConfig() Config {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return cfg
}

func updateConfig(fn func(*Config)) {
	cfgMu.Lock()
	fn(&cfg)
	cfgMu.Unlock()
	_ = saveConfig()
}
