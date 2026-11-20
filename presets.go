// presets.go — 内置预设背景的纯代码生成。
// 不依赖任何外部图片文件，全部用 image 包绘制，保证单文件分发。
package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	_ "embed"

	"golang.org/x/image/webp"
)

// Preset 描述一个内置背景。
type Preset struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// 缩略图为 data URI，供设置界面直接显示
	Thumb string `json:"thumb"`
}

var presetDefs = []struct {
	ID   string
	Name string
}{
	{"black", "纯黑"},
	{"carbon", "碳纹"},
	{"starfield", "星空"},
}

// renderPreset 生成指定预设的全尺寸图像。
func renderPreset(id string, w, h int) *image.RGBA {
	if w <= 0 {
		w = 1920
	}
	if h <= 0 {
		h = 1080
	}

	img := image.NewRGBA(image.Rect(0, 0, w, h))

	// exe 同目录 presets/ 下的图片作为动态预设（用户随时丢图进来）
	if strings.HasPrefix(id, "file:") {
		name := strings.TrimPrefix(id, "file:")
		if img, err := loadCustomImage(filepath.Join(presetsDir(), name)); err == nil {
			return coverImage(img, w, h)
		}
		return renderPreset("black", w, h)
	}

	switch id {
	case "moss":
		// 松烟：极暗青绿，与纯黑同量级的暗度
		gradientV(img, color.RGBA{12, 26, 24, 255}, color.RGBA{4, 9, 8, 255})
		radialGlow(img, 0.5, 1.0, 0.7, color.RGBA{30, 90, 70, 30})
	case "obsidian":
		// 曜石：深灰到黑的平滑渐变，顶部一抹冷光
		gradientV(img, color.RGBA{30, 31, 36, 255}, color.RGBA{6, 6, 8, 255})
		radialGlow(img, 0.5, 0.1, 0.6, color.RGBA{120, 130, 150, 26})
	case "carbon":
		gradientV(img, color.RGBA{52, 52, 56, 255}, color.RGBA{24, 24, 28, 255})
		diagonalWeave(img, 9, 4, color.RGBA{255, 255, 255, 16})
	case "starfield":
		// 星空：深蓝夜空 + 固定种子星点（保证每次渲染一致）+ 斜向银河微光
		gradientV(img, color.RGBA{6, 9, 20, 255}, color.RGBA{2, 3, 8, 255})
		// 银河微光：两团斜向淡蓝雾
		radialGlow(img, 0.32, 0.28, 0.55, color.RGBA{42, 62, 105, 30})
		radialGlow(img, 0.62, 0.48, 0.60, color.RGBA{52, 72, 115, 26})
		// 固定种子伪随机（LCG），保证遮罩重绘时星点不跳变
		seed := uint64(20261119)
		rnd := func() float64 {
			seed = seed*6364136223846793005 + 1442695040888963407
			return float64((seed>>33)&0x7FFFFFFF) / float64(0x7FFFFFFF)
		}
		// 星点尺寸与数量按分辨率缩放：以 1920x1080 为基准，
		// 高分屏上星点太小会像"花屏坏点"，必须等比放大
		sf := float64(w) / 1920.0
		if sf < 1 {
			sf = 1
		}
		smallN := int(600 * sf * sf)
		brightN := int(36 * sf * sf)
		starSize := int(2.0 * sf)
		if starSize < 1 {
			starSize = 1
		}
		half := starSize / 2

		// 小星：集中在中上部，亮度随机（画 starSize 方块）
		for i := 0; i < smallN; i++ {
			x := int(rnd() * float64(w))
			y := int(rnd() * float64(h) * 0.92)
			b := uint8(80 + rnd()*150)
			for dy := -half; dy <= half; dy++ {
				for dx := -half; dx <= half; dx++ {
					nx, ny := x+dx, y+dy
					if nx < 0 || nx >= w || ny < 0 || ny >= h {
						continue
					}
					// 圆形星点（方形点看着像坏像素）
					if math.Hypot(float64(dx), float64(dy)) > float64(half) {
						continue
					}
					c := img.RGBAAt(nx, ny)
					img.SetRGBA(nx, ny, color.RGBA{
						uint8(float64(c.R) + (255-float64(c.R))*float64(b)/255),
						uint8(float64(c.G) + (255-float64(c.G))*float64(b)/255),
						uint8(float64(c.B) + (255-float64(c.B))*float64(b)/255),
						255,
					})
				}
			}
		}
		// 亮星：核心 + 十字光晕（尺寸随 sf 放大）
		coreHalf := starSize
		haloR := starSize + 1
		for i := 0; i < brightN; i++ {
			x := int(rnd() * float64(w))
			y := int(rnd() * float64(h) * 0.85)
			for dy := -haloR; dy <= haloR; dy++ {
				for dx := -haloR; dx <= haloR; dx++ {
					nx, ny := x+dx, y+dy
					if nx < 0 || nx >= w || ny < 0 || ny >= h {
						continue
					}
					c := img.RGBAAt(nx, ny)
					if math.Abs(float64(dx)) <= float64(coreHalf) && math.Abs(float64(dy)) <= float64(coreHalf) {
						img.SetRGBA(nx, ny, color.RGBA{255, 255, 255, 255}) // 核心白
					} else if dx == 0 || dy == 0 {
						img.SetRGBA(nx, ny, color.RGBA{
							uint8(float64(c.R)*0.5 + 110),
							uint8(float64(c.G)*0.5 + 115),
							uint8(float64(c.B)*0.5 + 130),
							255,
						}) // 十字光晕
					}
				}
			}
		}
	default: // black — 最深的纯黑
		gradientV(img, color.RGBA{10, 10, 12, 255}, color.RGBA{2, 2, 3, 255})
	}
	return img
}

// gradientV 垂直线性渐变。
func gradientV(img *image.RGBA, top, bottom color.RGBA) {
	b := img.Bounds()
	h := b.Dy()
	for y := 0; y < h; y++ {
		t := 0.0
		if h > 1 {
			t = float64(y) / float64(h-1)
		}
		c := color.RGBA{
			R: lerp8(top.R, bottom.R, t),
			G: lerp8(top.G, bottom.G, t),
			B: lerp8(top.B, bottom.B, t),
			A: 255,
		}
		for x := 0; x < b.Dx(); x++ {
			img.SetRGBA(x, y, c)
		}
	}
}

// gradientVOver 在已有内容上叠加一层垂直渐变（保留 alpha 混合）。
func gradientVOver(img *image.RGBA, top, bottom color.RGBA) {
	b := img.Bounds()
	h := b.Dy()
	for y := 0; y < h; y++ {
		t := 0.0
		if h > 1 {
			t = float64(y) / float64(h-1)
		}
		a := lerp8(top.A, bottom.A, t)
		if a == 0 {
			continue
		}
		over := color.RGBA{0, 0, 0, a}
		for x := 0; x < b.Dx(); x++ {
			blendOver(img, x, y, over)
		}
	}
}

// diagonalWeave 绘制极淡的斜向纹理，让纯色背景不显得"程序挂了"。
func diagonalWeave(img *image.RGBA, step, width int, c color.RGBA) {
	b := img.Bounds()
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			if (x+y)%step < width {
				blendOver(img, x, y, c)
			}
		}
	}
}

// radialGlow 添加一个柔和的径向光晕。
func radialGlow(img *image.RGBA, cx, cy, radius float64, c color.RGBA) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	px, py := cx*float64(w), cy*float64(h)
	r := radius * float64(w)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := float64(x)-px, float64(y)-py
			d := math.Sqrt(dx*dx + dy*dy)
			if d > r {
				continue
			}
			k := 1.0 - d/r
			k = k * k // 平方衰减，边缘更柔
			blendOver(img, x, y, color.RGBA{c.R, c.G, c.B, uint8(float64(c.A) * k)})
		}
	}
}

func blendOver(img *image.RGBA, x, y int, c color.RGBA) {
	if c.A == 0 {
		return
	}
	dst := img.RGBAAt(x, y)
	a := float64(c.A) / 255.0
	img.SetRGBA(x, y, color.RGBA{
		R: uint8(float64(c.R)*a + float64(dst.R)*(1-a)),
		G: uint8(float64(c.G)*a + float64(dst.G)*(1-a)),
		B: uint8(float64(c.B)*a + float64(dst.B)*(1-a)),
		A: 255,
	})
}

func lerp8(a, b uint8, t float64) uint8 {
	v := float64(a) + (float64(b)-float64(a))*t
	if v < 0 {
		v = 0
	}
	if v > 255 {
		v = 255
	}
	return uint8(v)
}

func clamp8(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// coverImage 把 src 按 cover 语义缩放到 w×h（等比放大后居中裁切）。
// 避免 StretchBlt 直接拉伸造成的变形。
func coverImage(src image.Image, w, h int) *image.RGBA {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))

	if sw <= 0 || sh <= 0 {
		return dst
	}
	scale := math.Max(float64(w)/float64(sw), float64(h)/float64(sh))
	dw := int(math.Round(float64(sw) * scale))
	dh := int(math.Round(float64(sh) * scale))
	if dw < w {
		dw = w
	}
	if dh < h {
		dh = h
	}
	// 先缩放至 dw×dh（用最近邻以外的简单方式：逐像素双线性成本高，这里采用整数倍抽样的近似）
	scaled := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		sy := sb.Min.Y + int(float64(y)/scale)
		if sy >= sb.Max.Y {
			sy = sb.Max.Y - 1
		}
		for x := 0; x < dw; x++ {
			sx := sb.Min.X + int(float64(x)/scale)
			if sx >= sb.Max.X {
				sx = sb.Max.X - 1
			}
			scaled.SetRGBA(x, y, color.RGBAModel.Convert(src.At(sx, sy)).(color.RGBA))
		}
	}
	// 居中裁切
	ox := (dw - w) / 2
	oy := (dh - h) / 2
	draw.Draw(dst, image.Rect(0, 0, w, h), scaled, image.Pt(ox, oy), draw.Src)
	return dst
}

// loadCustomImage 读取并解码用户自定义图片（jpg / png / webp）。
func loadCustomImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpg", ".jpeg":
		return jpeg.Decode(f)
	case ".png":
		return png.Decode(f)
	case ".webp":
		return webp.Decode(f)
	default:
		// 兜底：按内容嗅探（webp 包 init 时已注册到 image.Decode）
		img, _, err := image.Decode(f)
		return img, err
	}
}

// ---- 缩略图缓存：LRU 上限 64 条（每张约 10KB，总量 <1MB）----

const thumbCacheMax = 64

var thumbCache = struct {
	mu    sync.Mutex
	m     map[string]string
	order []string
}{m: map[string]string{}}

func thumbCacheGet(k string) (string, bool) {
	thumbCache.mu.Lock()
	defer thumbCache.mu.Unlock()
	v, ok := thumbCache.m[k]
	return v, ok
}

func thumbCachePut(k, v string) {
	thumbCache.mu.Lock()
	defer thumbCache.mu.Unlock()
	if _, exists := thumbCache.m[k]; !exists {
		thumbCache.order = append(thumbCache.order, k)
	}
	thumbCache.m[k] = v
	// 超出上限时淘汰最旧的一半
	if len(thumbCache.order) > thumbCacheMax {
		drop := len(thumbCache.order) - thumbCacheMax/2
		for _, old := range thumbCache.order[:drop] {
			delete(thumbCache.m, old)
		}
		thumbCache.order = thumbCache.order[drop:]
	}
}

// thumbFor 统一的缩略图入口：内置预设与目录预设（file:）都走缓存。
func thumbFor(id string) string {
	if v, ok := thumbCacheGet(id); ok {
		return v
	}
	var uri string
	if strings.HasPrefix(id, "file:") {
		name := strings.TrimPrefix(id, "file:")
		uri = thumbDataURIfromFile(filepath.Join(presetsDir(), name))
	} else {
		uri = thumbDataURI(id)
	}
	thumbCachePut(id, uri)
	return uri
}

// thumbDataURI 生成预设缩略图的 data URI（供设置界面 <img> 使用）。
func thumbDataURI(id string) string {
	if v, ok := thumbCacheGet(id); ok {
		return v
	}
	img := renderPreset(id, 320, 180)
	buf := new(bytes.Buffer)
	_ = jpeg.Encode(buf, img, &jpeg.Options{Quality: 80})
	uri := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	thumbCachePut(id, uri)
	return uri
}

// presetList 返回带缩略图的所有预设：内置 6 张 + exe 同目录 presets/ 下的图片。
// 目录预设随时可加可删（删文件后重启程序生效），ID 前缀 file: 区分。
func presetList() []Preset {
	out := make([]Preset, 0, len(presetDefs))
	for _, d := range presetDefs {
		// 不在这里生成缩略图：goState 一次性回传全部 base64 会导致
		// WebView2 binding 大消息回传失败（预设看起来全空）。
		// 缩略图由前端按需调用 goThumb(id) 单独获取。
		out = append(out, Preset{ID: d.ID, Name: d.Name})
	}
	dir := presetsDir()
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" {
				continue
			}
			name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
			id := "file:" + e.Name()
			out = append(out, Preset{ID: id, Name: name})
		}
	}
	logf("INFO: 预设列表 %d 个（内置 %d + 目录 %s）", len(out), len(presetDefs), dir)
	return out
}

func presetName(id string) string {
	for _, d := range presetDefs {
		if d.ID == id {
			return d.Name
		}
	}
	return id
}

// currentBackground 依据配置返回当前应绘制的内容（图像 + 来源描述）。
// 全屏背景缓存：星空渲染 400 万像素要 200-500ms，每块屏各缓存一张，
// 切换配置后首次开启生成一次，之后秒开。
type bgCacheEntry struct {
	key string
	img *image.RGBA
	name string
}

var (
	bgCacheMu   sync.Mutex
	bgCacheList []bgCacheEntry
)

const bgCacheMax = 4

// renderOverride 预览时临时指定要渲染的配置（nil 表示按"生效配置"渲染）。
// atomic.Pointer 避免 UI 线程与渲染路径的竞态。
var renderOverride atomic.Pointer[renderSpec]

type renderSpec struct {
	PresetID    string
	CustomImage string
}

// specForRender 决定当前应渲染哪套配置：
//  1. 预览 override（用户点预览时临时显示选中项）
//  2. 生效配置 applied（遮罩正常状态始终显示"正在使用"的那套）
//  3. 兜底：选中配置（老配置没有 applied 字段时）
func specForRender() renderSpec {
	if ov := renderOverride.Load(); ov != nil {
		return *ov
	}
	c := getConfig()
	if c.AppliedPresetID != "" || c.AppliedCustomImage != "" {
		return renderSpec{c.AppliedPresetID, c.AppliedCustomImage}
	}
	return renderSpec{c.PresetID, c.CustomImage}
}

func currentBackground(w, h int) (*image.RGBA, string) {
	spec := specForRender()
	key := fmt.Sprintf("%s|%s|%d|%d", spec.PresetID, spec.CustomImage, w, h)

	bgCacheMu.Lock()
	for _, e := range bgCacheList {
		if e.key == key {
			bgCacheMu.Unlock()
			return e.img, e.name
		}
	}
	bgCacheMu.Unlock()

	var img *image.RGBA
	var name string
	if spec.CustomImage != "" {
		if im, err := loadCustomImage(spec.CustomImage); err == nil {
			img = coverImage(im, w, h)
			name = filepath.Base(spec.CustomImage)
		}
	}
	if img == nil {
		id := spec.PresetID
		if id == "" {
			id = "black"
		}
		img = renderPreset(id, w, h)
		name = presetName(id)
	}

	bgCacheMu.Lock()
	bgCacheList = append(bgCacheList, bgCacheEntry{key: key, img: img, name: name})
	if len(bgCacheList) > bgCacheMax {
		bgCacheList = bgCacheList[len(bgCacheList)-bgCacheMax:]
	}
	bgCacheMu.Unlock()
	return img, name
}
