package tui

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg" // poster formats served by TMDB and TVmaze
	_ "image/png"
	"strings"
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

// RenderImage draws img into a cols x rows block of terminal cells using the
// upper half block: the foreground colour is the top pixel and the background
// the bottom one. Colours are 24-bit; each pixel is the average of the source
// pixels it covers.
func RenderImage(img image.Image, cols, rows int) string {
	if img == nil || cols <= 0 || rows <= 0 {
		return ""
	}
	b := img.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		return ""
	}
	pxH := rows * 2
	var sb strings.Builder
	sb.Grow(rows * cols * 40)
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			tr, tg, tb := average(img, b, x, 2*y, cols, pxH)
			br, bg, bb := average(img, b, x, 2*y+1, cols, pxH)
			fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀", tr, tg, tb, br, bg, bb)
		}
		sb.WriteString("\x1b[0m")
		if y < rows-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// average returns the mean colour of the source area that maps to target
// pixel (tx, ty) in a tw x th grid.
func average(img image.Image, b image.Rectangle, tx, ty, tw, th int) (uint8, uint8, uint8) {
	x0 := b.Min.X + tx*b.Dx()/tw
	x1 := max(x0+1, b.Min.X+(tx+1)*b.Dx()/tw)
	y0 := b.Min.Y + ty*b.Dy()/th
	y1 := max(y0+1, b.Min.Y+(ty+1)*b.Dy()/th)
	var r, g, bl, n uint64
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			cr, cg, cb, _ := img.At(x, y).RGBA()
			r, g, bl, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), n+1
		}
	}
	if n == 0 {
		return 0, 0, 0
	}
	return uint8(r / n >> 8), uint8(g / n >> 8), uint8(bl / n >> 8)
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
