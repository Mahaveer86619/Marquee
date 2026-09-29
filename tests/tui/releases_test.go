package tui_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"marquee/internal/catalog"
	"marquee/internal/release"
	"marquee/internal/tui"
)

// filmBackend returns a film for Title so the film release path can be tested.
type filmBackend struct{ fakeBackend }

func (f *filmBackend) Title(_ context.Context, ref string) (catalog.Title, error) {
	return catalog.Title{Ref: ref, Kind: catalog.Movie, Name: "Second Film", Year: 2010}, nil
}

func openFilm(t *testing.T, fb *filmBackend) tea.Model {
	t.Helper()
	var m tea.Model = tui.New(fb)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 150, Height: 40})
	for _, k := range text("second") {
		m, _ = m.Update(k)
	}
	m = send(t, m, key(tea.KeyEnter)) // search
	m = send(t, m, key(tea.KeyDown))  // move to the film
	m = send(t, m, key(tea.KeyEnter)) // open it
	return m
}

func TestFilmReleasesArePrefetched(t *testing.T) {
	fb := &filmBackend{}
	m := openFilm(t, fb)
	// Opening the film already searched in the background...
	if len(fb.releaseQueries) != 1 || fb.releaseQueries[0].Scope != release.ScopeMovie {
		t.Fatalf("expected a background search on open, got %+v", fb.releaseQueries)
	}
	if v := plain(m.View().Content); !strings.Contains(v, "Releases: 1 ready (press d)") {
		t.Fatalf("status line should report prefetched releases:\n%s", v)
	}
	// ...so pressing d shows them without searching again.
	m = send(t, m, tea.KeyPressMsg{Code: 'd', Text: "d"})
	if len(fb.releaseQueries) != 1 {
		t.Fatalf("d searched again: %+v", fb.releaseQueries)
	}
	if v := plain(m.View().Content); !strings.Contains(v, "Second.Film.2010.1080p") {
		t.Fatalf("prefetched releases not shown:\n%s", v)
	}
}

func TestJunkIsHiddenUntilRequested(t *testing.T) {
	m := openFilm(t, &filmBackend{})
	m = send(t, m, tea.KeyPressMsg{Code: 'd', Text: "d"})
	v := plain(m.View().Content)
	if strings.Contains(v, "HDCAM") || !strings.Contains(v, "1 of 2 shown; 1 hidden") {
		t.Fatalf("camera copy should be hidden by default:\n%s", v)
	}
	m = send(t, m, tea.KeyPressMsg{Code: 'f', Text: "f"})
	if v := plain(m.View().Content); !strings.Contains(v, "HDCAM") {
		t.Fatalf("f should show hidden releases:\n%s", v)
	}
}

func TestReleasesAndOptionsFlow(t *testing.T) {
	fb := &filmBackend{}
	m := openFilm(t, fb)
	m = send(t, m, tea.KeyPressMsg{Code: 'd', Text: "d"})

	if len(fb.releaseQueries) != 1 || fb.releaseQueries[0].Scope != release.ScopeMovie {
		t.Fatalf("release queries = %+v", fb.releaseQueries)
	}
	v := plain(m.View().Content)
	for _, want := range []string{"Releases", "Second Film (2010)", "1080p", "Hindi, English", "Example Indexer", "H.264 plays in every browser"} {
		if !strings.Contains(v, want) {
			t.Fatalf("releases screen missing %q:\n%s", want, v)
		}
	}

	m = send(t, m, key(tea.KeyEnter)) // options
	v = plain(m.View().Content)
	if !strings.Contains(v, "[x] Hindi") || !strings.Contains(v, "[ ] English") || !strings.Contains(v, "[x] English") {
		// Hindi audio is preferred and pre-ticked; English audio is not; English subtitles are preferred.
		t.Fatalf("options not pre-ticked from preferences:\n%s", v)
	}

	m = send(t, m, key(tea.KeyDown))                      // English audio
	m = send(t, m, tea.KeyPressMsg{Code: ' ', Text: " "}) // tick it
	m = send(t, m, key(tea.KeyEnter))                     // confirm

	chosen := m.(tui.Model).Chosen()
	if chosen == nil || chosen.Release.ID != "r1" {
		t.Fatalf("selection = %+v", chosen)
	}
	if !slices.Equal(chosen.Audio, []string{"hi", "en"}) || !slices.Equal(chosen.Subtitles, []string{"en"}) {
		t.Fatalf("audio=%v subtitles=%v", chosen.Audio, chosen.Subtitles)
	}

	// Confirming queues the download with the core.
	if len(fb.started) != 1 {
		t.Fatalf("downloads started = %+v", fb.started)
	}
	if r := fb.started[0]; r.ReleaseID != "r1" || r.Ref != "tmdb:movie:2" || r.Scope != release.ScopeMovie || !slices.Equal(r.Audio, []string{"hi", "en"}) {
		t.Fatalf("download request = %+v", r)
	}
	if v := plain(m.View().Content); !strings.Contains(v, "Queued Second.Film.2010.1080p") {
		t.Fatalf("queued message missing:\n%s", v)
	}
}

func TestSeasonAndSeriesDownloadKeys(t *testing.T) {
	fb := &fakeBackend{}
	var m tea.Model = tui.New(fb)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 150, Height: 40})
	for _, k := range text("first") {
		m, _ = m.Update(k)
	}
	m = send(t, m, key(tea.KeyEnter)) // search
	m = send(t, m, key(tea.KeyEnter)) // open the series (cursor on season 1)

	m = send(t, m, tea.KeyPressMsg{Code: 'd', Text: "d"}) // releases for season 1
	m = send(t, m, key(tea.KeyEscape))
	m = send(t, m, tea.KeyPressMsg{Code: 'D', Text: "D"}) // releases for the whole series
	m = send(t, m, key(tea.KeyEscape))
	m = send(t, m, key(tea.KeyEnter))                     // season 1 episodes
	m = send(t, m, tea.KeyPressMsg{Code: 'd', Text: "d"}) // releases for S01E01

	want := []tui.ReleaseQuery{
		{Ref: "tmdb:tv:1", Scope: release.ScopeSeason, Season: 1},
		{Ref: "tmdb:tv:1", Scope: release.ScopeSeries},
		{Ref: "tmdb:tv:1", Scope: release.ScopeEpisode, Season: 1, Episode: 1},
	}
	if !slices.Equal(fb.releaseQueries, want) {
		t.Fatalf("queries = %+v\nwant    %+v", fb.releaseQueries, want)
	}
}
