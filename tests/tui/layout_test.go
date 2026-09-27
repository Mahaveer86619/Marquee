package tui_test

import (
	"image"
	"image/color"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"marquee/internal/tui"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func plain(s string) string { return ansi.ReplaceAllString(s, "") }

func TestRenderImageSize(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 60, 90))
	for y := 0; y < 90; y++ {
		for x := 0; x < 60; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: uint8(y), B: 40, A: 255})
		}
	}
	rows := 12
	cols := tui.PosterCols(rows)
	out := tui.RenderImage(img, cols, rows)
	lines := strings.Split(out, "\n")
	if len(lines) != rows {
		t.Fatalf("rendered %d rows, want %d", len(lines), rows)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != cols {
			t.Fatalf("row %d is %d cells wide, want %d", i, w, cols)
		}
	}
	if !strings.Contains(out, "\x1b[38;2;") || !strings.Contains(out, "\x1b[48;2;") {
		t.Fatal("expected 24-bit foreground and background colours")
	}
}

func TestRenderImageEmpty(t *testing.T) {
	if tui.RenderImage(nil, 10, 10) != "" {
		t.Fatal("nil image should render nothing")
	}
}

func searchAndPreview(t *testing.T, w, h int) tea.Model {
	t.Helper()
	var m tea.Model = tui.New(&fakeBackend{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	for _, k := range text("first") {
		m, _ = m.Update(k)
	}
	m = send(t, m, key(tea.KeyEnter)) // search; the preview is scheduled with a short delay
	return m
}

func TestViewFillsTerminalHeight(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {100, 30}, {80, 24}} {
		m := searchAndPreview(t, size[0], size[1])
		lines := strings.Split(m.View().Content, "\n")
		if len(lines) != size[1] {
			t.Fatalf("%dx%d: view has %d lines, want %d", size[0], size[1], len(lines), size[1])
		}
		for i, l := range lines {
			if w := lipgloss.Width(l); w > size[0] {
				t.Fatalf("%dx%d: line %d is %d cells wide: %q", size[0], size[1], i, w, plain(l))
			}
		}
	}
}

func TestPreviewShowsFacts(t *testing.T) {
	m := searchAndPreview(t, 160, 45)
	v := plain(m.View().Content)
	for _, want := range []string{"First Show (2008)", "First aired", "8 Jan 2008", "Rating", "7.9"} {
		if !strings.Contains(v, want) {
			t.Fatalf("preview missing %q:\n%s", want, v)
		}
	}
}
