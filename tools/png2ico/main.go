// png2ico —— 把一张 PNG 转成多尺寸 Windows ICO。
//
// 用法:
//
//	go run ./tools/png2ico -in logo.png -out app.ico -crop 0.08
//
// -crop 会从底部裁掉指定比例（用于去掉生成图右下角的水印），
// 裁完再按正方形居中裁剪，保证图标不是拉伸变形。
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
)

var sizes = []int{16, 24, 32, 48, 64, 128, 256}

func main() {
	in := flag.String("in", "", "输入 PNG")
	out := flag.String("out", "app.ico", "输出 ICO")
	crop := flag.Float64("crop", 0, "从底部裁掉的比例，例如 0.08")
	flag.Parse()

	if *in == "" {
		fmt.Println("缺少 -in")
		os.Exit(1)
	}

	f, err := os.Open(*in)
	if err != nil {
		fmt.Println("打开失败:", err)
		os.Exit(1)
	}
	src, err := png.Decode(f)
	f.Close()
	if err != nil {
		fmt.Println("解码失败:", err)
		os.Exit(1)
	}

	// 1) 裁掉底部（水印区）
	if *crop > 0 {
		b := src.Bounds()
		keep := int(float64(b.Dy()) * (1 - *crop))
		src = cropRect(src, image.Rect(b.Min.X, b.Min.Y, b.Max.X, b.Min.Y+keep))
	}

	// 2) 居中裁成正方形
	src = centerSquare(src)

	// 3) 生成各尺寸 PNG
	type entry struct {
		size int
		data []byte
	}
	var entries []entry
	for _, s := range sizes {
		img := resize(src, s, s)
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			fmt.Println("编码失败:", err)
			os.Exit(1)
		}
		entries = append(entries, entry{s, buf.Bytes()})
	}

	// 4) 写 ICO
	var outBuf bytes.Buffer
	binary.Write(&outBuf, binary.LittleEndian, uint16(0)) // reserved
	binary.Write(&outBuf, binary.LittleEndian, uint16(1)) // type = icon
	binary.Write(&outBuf, binary.LittleEndian, uint16(len(entries)))

	offset := 6 + 16*len(entries)
	for _, e := range entries {
		w := byte(e.size)
		if e.size >= 256 {
			w = 0 // 256 在 ICO 里记作 0
		}
		outBuf.WriteByte(w)                                       // width
		outBuf.WriteByte(w)                                       // height
		outBuf.WriteByte(0)                                       // color count
		outBuf.WriteByte(0)                                       // reserved
		binary.Write(&outBuf, binary.LittleEndian, uint16(1))     // planes
		binary.Write(&outBuf, binary.LittleEndian, uint16(32))    // bit count
		binary.Write(&outBuf, binary.LittleEndian, uint32(len(e.data)))
		binary.Write(&outBuf, binary.LittleEndian, uint32(offset))
		offset += len(e.data)
	}
	for _, e := range entries {
		outBuf.Write(e.data)
	}

	if err := os.WriteFile(*out, outBuf.Bytes(), 0644); err != nil {
		fmt.Println("写入失败:", err)
		os.Exit(1)
	}
	fmt.Printf("OK: %s (%d 字节, %d 个尺寸)\n", *out, outBuf.Len(), len(entries))
}

func cropRect(src image.Image, r image.Rectangle) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	for y := 0; y < r.Dy(); y++ {
		for x := 0; x < r.Dx(); x++ {
			dst.Set(x, y, src.At(r.Min.X+x, r.Min.Y+y))
		}
	}
	return dst
}

func centerSquare(src image.Image) *image.RGBA {
	b := src.Bounds()
	side := b.Dx()
	if b.Dy() < side {
		side = b.Dy()
	}
	ox := b.Min.X + (b.Dx()-side)/2
	oy := b.Min.Y + (b.Dy()-side)/2
	return cropRect(src, image.Rect(ox, oy, ox+side, oy+side))
}

// resize 用面积平均缩小（box filter），对图标足够，且不会产生锯齿。
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
