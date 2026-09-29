package tui_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"marquee/internal/download"
	"marquee/internal/tui"
)

func TestDownloadsScreen(t *testing.T) {
	fb := &fakeBackend{downloads: []download.View{
		{
			Download: download.Download{ID: "d1", Name: "Night.of.the.Living.Dead.1968", State: download.Downloading, BytesDone: 250 << 20, BytesTotal: 500 << 20},
			Progress: 0.5, RateBps: 2 << 20, ETASeconds: 125, Peers: 4,
			Files: []download.File{{Index: 0, Path: "night/Night.mp4", Kind: "video", Size: 500 << 20, BytesDone: 250 << 20, Selected: true}, {Index: 1, Path: "cover.jpg", Kind: "other"}},
		},
		{Download: download.Download{ID: "d2", Name: "Broken.Release", State: download.Failed, Error: "tracker unreachable"}},
	}}
	var m tea.Model = tui.New(fb).WithPollInterval(time.Millisecond)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 150, Height: 40})

	m = send(t, m, tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	v := plain(view(m))
	for _, want := range []string{"Downloads", "Night.of.the.Living.Dead.1968", "50.0%", "Downloading", "2 MB/s", "2m 05s left", "4 peers",
		"Failed", "tracker unreachable", "night/Night.mp4", "1 other files in the torrent are not downloaded"} {
		if !strings.Contains(v, want) {
			t.Fatalf("downloads screen missing %q:\n%s", want, v)
		}
	}

	m = send(t, m, tea.KeyPressMsg{Code: 'p', Text: "p"}) // pause d1
	m = send(t, m, key(tea.KeyDown))
	m = send(t, m, tea.KeyPressMsg{Code: 'p', Text: "p"}) // retry d2
	m = send(t, m, key(tea.KeyUp))
	m = send(t, m, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if v := plain(view(m)); !strings.Contains(v, "press x again") {
		t.Fatalf("cancel should ask for confirmation:\n%s", v)
	}
	m = send(t, m, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if want := []string{"pause d1", "resume d2", "cancel d1"}; !slices.Equal(fb.actions, want) {
		t.Fatalf("actions = %v, want %v", fb.actions, want)
	}

	m = send(t, m, key(tea.KeyEscape))
	if v := plain(view(m)); !strings.Contains(v, "type to search") {
		t.Fatalf("esc should return to search:\n%s", v)
	}
}

func TestDownloadsScreenEmpty(t *testing.T) {
	var m tea.Model = tui.New(&fakeBackend{}).WithPollInterval(time.Millisecond)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = send(t, m, tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if v := plain(view(m)); !strings.Contains(v, "Nothing downloaded yet") {
		t.Fatalf("empty state missing:\n%s", v)
	}
}
