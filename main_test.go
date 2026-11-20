package main

import (
	"encoding/json"
	"image/png"
	"os"
	"testing"
)

// 回归测试：png.Decode 返回 *image.NRGBA，此前硬断言 *image.RGBA 导致右键托盘时 panic。
func TestMakeEyeIcon(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("makeEyeIcon panic: %v", r)
		}
	}()
	h := makeEyeIcon()
	if h == 0 {
		t.Fatal("makeEyeIcon 返回了空句柄")
	}
}

// 渲染星空预设预览供人工检查（go test -run TestPreviewStarfield）
func TestPreviewStarfield(t *testing.T) {
	img := renderPreset("starfield", 480, 300)
	f, err := os.Create("starfield-preview.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func TestParseHotkey(t *testing.T) {
	cases := []struct {
		in    string
		ok    bool
	}{
		{"Ctrl+Alt+V", true},
		{"Ctrl+Alt+Shift+Q", true},
		{"Alt+F5", true},
		{"Ctrl+Alt+X", true},
		{"V", false},                // 缺修饰键
		{"Ctrl", false},             // 缺主键
		{"Ctrl+Alt+Shift", false},   // 缺主键
		{"Ctrl+Alt+F13", false},     // 超范围
	}
	for _, c := range cases {
		_, _, err := parseHotkey(c.in)
		if (err == nil) != c.ok {
			t.Errorf("parseHotkey(%q) err=%v, 期望 ok=%v", c.in, err, c.ok)
		}
	}
}

// 验证 goState 的回传体积——超过 WebView2 binding 可靠范围会导致前端拿到空预设
func TestStatePayloadSize(t *testing.T) {
	b, err := json.Marshal(buildState())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("goState JSON: %d 字节（%d 个预设）", len(b), len(buildState().Presets))
	if len(b) > 8192 {
		t.Fatalf("goState 仍然过大 (%d 字节)，大消息会导致预设空", len(b))
	}
	for _, p := range buildState().Presets {
		if p.Thumb != "" {
			t.Fatalf("预设 %s 仍带 thumb 字段（应走 goThumb 按需加载）", p.ID)
		}
	}
}

// 验证单张缩略图体积与生成耗时（goThumb 单次消息）
func TestThumbPayloadSize(t *testing.T) {
	s := thumbDataURI("carbon")
	t.Logf("单张缩略图: %d 字节", len(s))
	if len(s) < 1000 || len(s) > 200000 {
		t.Fatalf("缩略图体积异常: %d", len(s))
	}
}

// 替身光标渲染：系统箭头光标必须渲染出可见像素（同时可用来确认系统光标没有停留在透明状态）。
func TestRenderCursorShape(t *testing.T) {
	h, _, _ := procLoadCursorW.Call(0, 32512)
	s := renderCursorShape(h)
	if s == nil {
		t.Fatal("renderCursorShape 返回 nil")
	}
	pix := s.pix
	opaque, partial := 0, 0
	for i := 3; i < len(pix); i += 4 {
		if pix[i] == 255 {
			opaque++
		} else if pix[i] > 0 {
			partial++
		}
	}
	t.Logf("箭头光标 %dx%d 热点(%d,%d) 不透明像素=%d 半透明=%d", s.w, s.h, s.hotX, s.hotY, opaque, partial)
	if opaque == 0 {
		t.Fatal("箭头光标没有任何不透明像素")
	}
	if opaque == int(s.w)*int(s.h) {
		t.Fatal("整块位图都不透明：alpha 反推失败")
	}
}
