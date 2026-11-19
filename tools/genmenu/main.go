// genmenu — 生成菜单图标 PNG：用户提供的 SVG 1:1 栅格化（32x32，白底）。
// 白底原因：SetMenuItemInfo 的 hbmpItem 不支持 per-pixel alpha，
// 透明像素会被渲染成黑色，所以图标文件本身就用不透明白底。
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

func distToSeg(px, py, x1, y1, x2, y2 float64) float64 {
	dx, dy := x2-x1, y2-y1
	l2 := dx*dx + dy*dy
	if l2 == 0 {
		return math.Hypot(px-x1, py-y1)
	}
	t := ((px-x1)*dx + (py-y1)*dy) / l2
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	return math.Hypot(px-(x1+t*dx), py-(y1+t*dy))
}

// sdRoundedRect 圆角矩形的有符号距离场（负=内部，正=外部）。
// 旧实现对外部点正确，但对内部点返回 -r 导致整个矩形内部被误填充。
func sdRoundedRect(px, py, x0, y0, w, h, r float64) float64 {
	cx, cy := x0+w/2, y0+h/2
	bw, bh := w/2-r, h/2-r
	dx := math.Abs(px-cx) - bw
	dy := math.Abs(py-cy) - bh
	ox, oy := math.Max(dx, 0), math.Max(dy, 0)
	outside := math.Hypot(ox, oy)
	inside := math.Min(math.Max(dx, dy), 0)
	return outside + inside - r
}

var sizeFlag = flag.Int("size", 32, "输出尺寸")
var outPath = flag.String("out", "assets/menu/eye.png", "输出路径")

func main() {
	flag.Parse()
	s := *sizeFlag
	// SVG viewBox 是 32x32，任意尺寸输出都按比例映射坐标
	scale := float64(s) / 32.0
	img := image.NewRGBA(image.Rect(0, 0, s, s))

	for y := 0; y < s; y++ {
		for x := 0; x < s; x++ {
			img.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
		}
	}

	var bez [][2]float64
	for i := 0; i <= 96; i++ {
		t := float64(i) / 96
		mt := 1 - t
		bez = append(bez, [2]float64{
			mt*mt*8 + 2*mt*t*16 + t*t*24,
			mt*mt*13.5 + 2*mt*t*20.5 + t*t*13.5,
		})
	}

	type segT struct{ x1, y1, x2, y2 float64 }
	segs := []segT{
		{11.5, 15.5, 9.7, 18.7},
		{16, 16.7, 16, 19.7},
		{20.5, 15.5, 22.3, 18.7},
	}

	ink := color.RGBA{0x1c, 0x1c, 0x1c, 255}
	half := 1.25

	for y := 0; y < s; y++ {
		for x := 0; x < s; x++ {
			// 像素中心 → SVG 坐标（除以缩放比）
			px, py := (float64(x)+0.5)/scale, (float64(y)+0.5)/scale
			d := math.Abs(sdRoundedRect(px, py, 4, 4, 24, 24, 5))
			for _, p := range bez {
				if dd := math.Hypot(px-p[0], py-p[1]); dd < d {
					d = dd
				}
			}
			for _, sg := range segs {
				if dd := distToSeg(px, py, sg.x1, sg.y1, sg.x2, sg.y2); dd < d {
					d = dd
				}
			}
			if d <= half {
				aa := uint8(255 - int(math.Min(1, (half-d))*160))
				img.SetRGBA(x, y, color.RGBA{ink.R, ink.G, ink.B, aa})
			}
		}
	}

	f, err := os.Create(*outPath)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
	fmt.Printf("OK: %s (%dx%d 白底)\n", *outPath, s, s)
}
