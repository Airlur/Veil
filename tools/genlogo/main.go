// genlogo — Veil 图标生成器：黑底 + 白色实心 V 字形（外缘带柔和光晕）。
// 形状直接来自用户提供的 SVG path：
//   M136 132 L210 132 L256 356 L302 132 L376 132 L294 392 L218 392 Z  (512x512)
// 通过点在多边形内测试精确还原形状，外缘用距离场做光晕，任意尺寸锐利。
//
// 用法：go run ./tools/genlogo -out .
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

// SVG path 的多边形顶点（512x512 坐标系）
var poly = [][2]float64{
	{136, 132},
	{210, 132},
	{256, 356},
	{302, 132},
	{376, 132},
	{294, 392},
	{218, 392},
}

var (
	outDir  = flag.String("out", ".", "输出目录")
	glowStr = flag.Float64("glow", 0.55, "外部光晕强度 0~1")
)

// sdRoundedRect 圆角矩形有符号距离场（负=内部，正=外部）。
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

func main() {
	flag.Parse()
	const size = 512
	img := renderLogo(size)

	if err := os.MkdirAll(*outDir, 0755); err != nil {
		panic(err)
	}
	pngPath := *outDir + "/veil-logo.png"
	f, err := os.Create(pngPath)
	if err != nil {
		panic(err)
	}
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
	f.Close()
	fmt.Println("PNG:", pngPath)

	icoPath := *outDir + "/app.ico"
	if err := writeICO(icoPath, img); err != nil {
		panic(err)
	}
	st, _ := os.Stat(icoPath)
	fmt.Printf("ICO: %s (%d 字节)\n", icoPath, st.Size())
}

// renderLogo 生成 size×size 图标（512 坐标系等比缩放）。
func renderLogo(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	S := float64(size) / 512.0 // SVG 坐标 → 像素 缩放

	glowSigma := 26.0 * S

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			px, py := (float64(x)+0.5)/S, (float64(y)+0.5)/S

			var r, g, b float64
			if pointInPoly(px, py) {
				// V 内部：纯白
				r, g, b = 255, 255, 255
			} else {
				// 外部：黑底 + 沿边缘的光晕
				d := distToPolyEdge(px, py)
				v := *glowStr * math.Exp(-d*d/(2*glowSigma*glowSigma))
				r = 255 * v
				g = 255 * v
				b = 255 * v
			}
			// 圆角裁剪：角外透明（现代 Windows 图标惯例的圆角方形轮廓）
			// px/py 是 SVG 坐标（0..512），画布参数必须同坐标系
			a := uint8(255)
			dEdge := sdRoundedRect(px, py, 0, 0, 512, 512, 82)
			if dEdge >= 0.75 {
				a = 0
			} else if dEdge > -0.75 {
				a = uint8(255 * (0.75 - dEdge) / 1.5)
			}
			if a == 0 {
				r, g, b = 0, 0, 0 // 全透明像素 RGB 归零，避免预乘歧义
			}
			img.SetRGBA(x, y, color.RGBA{clamp8(r), clamp8(g), clamp8(b), a})
		}
	}
	return img
}

// pointInPoly 射线法：点是否在多边形内。
func pointInPoly(px, py float64) bool {
	inside := false
	n := len(poly)
	j := n - 1
	for i := 0; i < n; i++ {
		xi, yi := poly[i][0], poly[i][1]
		xj, yj := poly[j][0], poly[j][1]
		if (yi > py) != (yj > py) &&
			px < (xj-xi)*(py-yi)/(yj-yi)+xi {
			inside = !inside
		}
		j = i
	}
	return inside
}

// distToPolyEdge 点到多边形各边的最小距离。
func distToPolyEdge(px, py float64) float64 {
	min := math.MaxFloat64
	n := len(poly)
	j := n - 1
	for i := 0; i < n; i++ {
		d := distToSeg(px, py, poly[j][0], poly[j][1], poly[i][0], poly[i][1])
		if d < min {
			min = d
		}
		j = i
	}
	return min
}

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

func clamp8(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// writeICO 输出多尺寸 ICO（内嵌 PNG，Vista+ 支持）。
func writeICO(path string, src image.Image) error {
	sizes := []int{16, 24, 32, 48, 64, 128, 256}
	type entry struct {
		size int
		data []byte
	}
	var entries []entry
	for _, s := range sizes {
		img := resize(src, s, s)
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return err
		}
		entries = append(entries, entry{s, buf.Bytes()})
	}

	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, uint16(0))
	binary.Write(&out, binary.LittleEndian, uint16(1))
	binary.Write(&out, binary.LittleEndian, uint16(len(entries)))

	offset := 6 + 16*len(entries)
	for _, e := range entries {
		w := byte(e.size)
		if e.size >= 256 {
			w = 0
		}
		out.WriteByte(w)
		out.WriteByte(w)
		out.WriteByte(0)
		out.WriteByte(0)
		binary.Write(&out, binary.LittleEndian, uint16(1))
		binary.Write(&out, binary.LittleEndian, uint16(32))
		binary.Write(&out, binary.LittleEndian, uint32(len(e.data)))
		binary.Write(&out, binary.LittleEndian, uint32(offset))
		offset += len(e.data)
	}
	for _, e := range entries {
		out.Write(e.data)
	}
	return os.WriteFile(path, out.Bytes(), 0644)
}

// resize 面积平均缩放。
func resize(src image.Image, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	for y := 0; y < h; y++ {
		y0 := sb.Min.Y + y*sh/h
		y1 := sb.Min.Y + (y+1)*sh/h
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < w; x++ {
			x0 := sb.Min.X + x*sw/w
			x1 := sb.Min.X + (x+1)*sw/w
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var rs, gs, bs, as, n uint64
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					c := color.NRGBAModel.Convert(src.At(xx, yy)).(color.NRGBA)
					rs += uint64(c.R)
					gs += uint64(c.G)
					bs += uint64(c.B)
					as += uint64(c.A)
					n++
				}
			}
			if n == 0 {
				n = 1
			}
			dst.Set(x, y, color.NRGBA{uint8(rs / n), uint8(gs / n), uint8(bs / n), uint8(as / n)})
		}
	}
	return dst
}
