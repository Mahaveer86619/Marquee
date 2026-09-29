package tui_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"marquee/internal/catalog"
	"marquee/internal/download"
	"marquee/internal/release"
	"marquee/internal/tui"
)

type fakeBackend struct {
	queries        []string
	releaseQueries []tui.ReleaseQuery
	started        []tui.DownloadRequest
	actions        []string
	downloads      []download.View
}

func (f *fakeBackend) StartDownload(_ context.Context, r tui.DownloadRequest) (download.Download, error) {
	f.started = append(f.started, r)
	return download.Download{ID: "d1", Name: "Second.Film.2010.1080p", State: download.Queued}, nil
}

func (f *fakeBackend) Downloads(context.Context) ([]download.View, error) { return f.downloads, nil }

func (f *fakeBackend) DownloadAction(_ context.Context, id, action string) (download.View, error) {
	f.actions = append(f.actions, action+" "+id)
	for _, v := range f.downloads {
		if v.ID == id {
			return v, nil
		}
	}
	return download.View{}, nil
}

func (f *fakeBackend) Status(context.Context) (tui.Status, error) {
	return tui.Status{Checks: map[string]string{"tmdb": "valid"}}, nil
}

func (f *fakeBackend) Search(_ context.Context, q string) (tui.SearchResponse, error) {
	f.queries = append(f.queries, q)
	return tui.SearchResponse{Source: "tmdb", Results: []catalog.SearchResult{
		{Ref: "tmdb:tv:1", Kind: catalog.Series, Name: "First Show", Year: 2008, ReleaseDate: "2008-01-08", Rating: 7.9},
		{Ref: "tmdb:movie:2", Kind: catalog.Movie, Name: "Second Film", Year: 2010},
	}}, nil
}

func (f *fakeBackend) Title(_ context.Context, ref string) (catalog.Title, error) {
	return catalog.Title{Ref: ref, Kind: catalog.Series, Name: "First Show", Year: 2008,
		Seasons: []catalog.Season{{Number: 0, Name: "Specials"}, {Number: 1, Name: "Season 1", EpisodeCount: 2}}}, nil
}

func (f *fakeBackend) Episodes(_ context.Context, ref string, season int) ([]catalog.Episode, error) {
	return []catalog.Episode{
		{Ref: catalog.EpisodeRef(ref, season, 1), Season: season, Number: 1, Name: "Pilot"},
		{Ref: catalog.EpisodeRef(ref, season, 2), Season: season, Number: 2, Name: "Second"},
	}, nil
}

// Poster returns a generated 20x30 gradient PNG (no real artwork in tests).
func (f *fakeBackend) Poster(context.Context, string) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, 20, 30))
	for y := 0; y < 30; y++ {
		for x := 0; x < 20; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 12), G: uint8(y * 8), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (f *fakeBackend) Releases(_ context.Context, q tui.ReleaseQuery) (tui.ReleasesResponse, error) {
	f.releaseQueries = append(f.releaseQueries, q)
	var r tui.ReleasesResponse
	r.Target = release.Target{Ref: q.Ref, Scope: q.Scope}
	r.Sources = []release.SourceStatus{{Name: "internet_archive", Status: "ok", Count: 1}}
	r.Preferences.AudioLanguages = []string{"hi"}
	r.Preferences.SubtitleLanguages = []string{"en"}
	r.Releases = []release.Release{{
		ID: "r1", Source: "prowlarr", Indexer: "Example Indexer", Name: "Second.Film.2010.1080p.WEB-DL.x264 [Hindi + English] ESub",
		SizeBytes: 3 << 30, Seeders: 88, Score: 120, Reasons: []string{"+40 1080p", "+30 H.264 plays in every browser"},
		Parsed: release.Parsed{Resolution: "1080p", Codec: "avc", Languages: []string{"hi", "en"}, Subtitles: []string{"en"}},
	}, {
		ID: "junk", Source: "prowlarr", Name: "Second.Film.2010.HDCAM.x264", Seeders: 5, Score: -150,
		Parsed: release.Parsed{Quality: "CAM", Trash: true},
	}}
	return r, nil
}

func key(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

func text(s string) []tea.KeyPressMsg {
	var out []tea.KeyPressMsg
	for _, r := range s {
		out = append(out, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return out
}

// send feeds a message, runs the returned commands synchronously (including
// batches, which carry background work such as prefetches) and feeds their
// results back in, a few levels deep.
func send(t *testing.T, m tea.Model, msg tea.Msg) tea.Model {
	t.Helper()
	m, cmd := m.Update(msg)
	return run(m, cmd, 3)
}

func run(m tea.Model, cmd tea.Cmd, depth int) tea.Model {
	if cmd == nil || depth == 0 {
		return m
	}
	switch out := cmd().(type) {
	case nil:
		return m
	case tea.BatchMsg:
		for _, c := range out {
			m = run(m, c, depth)
		}
		return m
	default:
		next, more := m.Update(out)
		return run(next, more, depth-1)
	}
}

func view(m tea.Model) string { return m.View().Content }

func TestSearchOpenTitleAndSeason(t *testing.T) {
	fb := &fakeBackend{}
	var m tea.Model = tui.New(fb)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	for _, k := range text("first") {
		m, _ = m.Update(k) // typing schedules a debounced search; Enter searches immediately
	}
	m = send(t, m, key(tea.KeyEnter))
	if len(fb.queries) != 1 || fb.queries[0] != "first" {
		t.Fatalf("queries = %v", fb.queries)
	}
	if v := view(m); !strings.Contains(v, "First Show (2008)") || !strings.Contains(v, "Second Film (2010)") {
		t.Fatalf("results not shown:\n%s", v)
	}

	m = send(t, m, key(tea.KeyEnter)) // results list is focused after Enter; open the first result
	if v := view(m); !strings.Contains(v, "Seasons") || !strings.Contains(v, "Season 1") {
		t.Fatalf("title screen not shown:\n%s", v)
	}

	m = send(t, m, key(tea.KeyEnter)) // cursor starts on season 1, not specials
	if v := view(m); !strings.Contains(v, "Season 1") || !strings.Contains(v, "S01E01") || !strings.Contains(v, "Pilot") {
		t.Fatalf("season screen not shown:\n%s", v)
	}

	m = send(t, m, key(tea.KeyEscape))
	m = send(t, m, key(tea.KeyEscape))
	if v := view(m); !strings.Contains(v, "results from tmdb") {
		t.Fatalf("back navigation did not return to results:\n%s", v)
	}
}

func TestEmptyEnterDoesNotSearch(t *testing.T) {
	fb := &fakeBackend{}
	var m tea.Model = tui.New(fb)
	m = send(t, m, key(tea.KeyEnter))
	if len(fb.queries) != 0 {
		t.Fatalf("searched with an empty query: %v", fb.queries)
	}
}

func TestViewShowsAttributionAndStatus(t *testing.T) {
	var m tea.Model = tui.New(&fakeBackend{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	v := view(m)
	if !strings.Contains(v, "not endorsed or certified by TMDB") {
		t.Fatalf("TMDB notice missing:\n%s", v)
	}
	if !m.View().AltScreen {
		t.Fatal("expected the alternate screen")
	}
}
