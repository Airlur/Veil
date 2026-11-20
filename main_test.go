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
