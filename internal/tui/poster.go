package tui

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg" // poster formats served by TMDB and TVmaze
	_ "image/png"
	"math"
	"strings"

	xdraw "golang.org/x/image/draw"
)

// DecodeImage decodes a JPEG or PNG image.
func DecodeImage(data []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

// PosterCols returns the width in cells of a poster drawn in rows cells:
// each cell holds two pixels vertically, and posters are 2:3.
func PosterCols(rows int) int {
	return max(1, rows*2*2/3)
}

// RenderImage draws img into a cols x rows block of terminal cells. Each cell
// is an upper half block: the foreground colour is the top pixel and the
// background the bottom one, so the effective resolution is cols x 2*rows.
//
// Quality steps: the source is first reduced to 2x the target in linear light
// (gamma-correct box filter, so fine detail averages without darkening), then
// resampled to the target with a Catmull-Rom filter for sharp edges.
func RenderImage(img image.Image, cols, rows int) string {
	if img == nil || cols <= 0 || rows <= 0 {
		return ""
	}
	if b := img.Bounds(); b.Dx() == 0 || b.Dy() == 0 {
		return ""
	}
	pw, ph := cols, rows*2
	mid := linearBoxResize(img, pw*2, ph*2)
	dst := image.NewRGBA(image.Rect(0, 0, pw, ph))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), mid, mid.Bounds(), xdraw.Src, nil)

	var sb strings.Builder
	sb.Grow(rows * cols * 40)
	for y := range rows {
		for x := range cols {
			t := dst.RGBAAt(x, 2*y)
			b := dst.RGBAAt(x, 2*y+1)
			fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀", t.R, t.G, t.B, b.R, b.G, b.B)
		}
		sb.WriteString("\x1b[0m")
		if y < rows-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// sRGB <-> linear lookup tables (8-bit sRGB to linear [0,1], and back).
var (
	toLinear [256]float64
)

func init() {
	for i := range toLinear {
		c := float64(i) / 255
		if c <= 0.04045 {
			toLinear[i] = c / 12.92
		} else {
			toLinear[i] = math.Pow((c+0.055)/1.055, 2.4)
		}
	}
}

func toSRGB(l float64) uint8 {
	var c float64
	if l <= 0.0031308 {
		c = l * 12.92
	} else {
		c = 1.055*math.Pow(l, 1/2.4) - 0.055
	}
	return uint8(math.Round(math.Min(1, math.Max(0, c)) * 255))
}

// linearBoxResize averages source pixels in linear light into a w x h image.
// When the source is already smaller, it is returned unchanged.
func linearBoxResize(img image.Image, w, h int) image.Image {
	b := img.Bounds()
	if b.Dx() <= w || b.Dy() <= h {
		return img
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for ty := range h {
		y0 := b.Min.Y + ty*b.Dy()/h
		y1 := max(y0+1, b.Min.Y+(ty+1)*b.Dy()/h)
		for tx := range w {
			x0 := b.Min.X + tx*b.Dx()/w
			x1 := max(x0+1, b.Min.X+(tx+1)*b.Dx()/w)
			var r, g, bl float64
			n := 0
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					cr, cg, cb, _ := img.At(x, y).RGBA()
					r += toLinear[cr>>8]
					g += toLinear[cg>>8]
					bl += toLinear[cb>>8]
					n++
				}
			}
			fn := float64(n)
			i := out.PixOffset(tx, ty)
			out.Pix[i+0] = toSRGB(r / fn)
			out.Pix[i+1] = toSRGB(g / fn)
			out.Pix[i+2] = toSRGB(bl / fn)
			out.Pix[i+3] = 255
		}
	}
	return out
}

// placeholderPoster draws an empty frame of the poster's size with a label.
func placeholderPoster(cols, rows int, label string) string {
	if cols < 3 || rows < 3 {
		return ""
	}
	lines := make([]string, rows)
	inner := cols - 2
	lines[0] = "+" + strings.Repeat("-", inner) + "+"
	lines[rows-1] = lines[0]
	for i := 1; i < rows-1; i++ {
		lines[i] = "|" + strings.Repeat(" ", inner) + "|"
	}
	if mid := rows / 2; label != "" && inner >= len(label) {
		pad := (inner - len(label)) / 2
		lines[mid] = "|" + strings.Repeat(" ", pad) + label + strings.Repeat(" ", inner-pad-len(label)) + "|"
	}
	return styleDim.Render(strings.Join(lines, "\n"))
}
