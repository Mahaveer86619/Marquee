package tui_test

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"marquee/internal/release"
	"marquee/internal/tui"
)

// twoSourceBackend reports two configured release sources and answers each
// source's request separately.
type twoSourceBackend struct{ filmBackend }

func (b *twoSourceBackend) Status(context.Context) (tui.Status, error) {
	return tui.Status{
		Checks: map[string]string{"tmdb": "valid"},
		ReleaseSources: []release.SourceInfo{
			{Name: "internet_archive", Configured: true},
			{Name: "prowlarr", Configured: true},
			{Name: "extra", Detail: "not enabled"},
		},
	}, nil
}

func (b *twoSourceBackend) Releases(_ context.Context, q tui.ReleaseQuery) (tui.ReleasesResponse, error) {
	b.releaseQueries = append(b.releaseQueries, q)
	var r tui.ReleasesResponse
	r.Target = release.Target{Ref: q.Ref, Scope: q.Scope}
	switch q.Source {
	case "internet_archive":
		r.Sources = []release.SourceStatus{{Name: "internet_archive", Status: "ok", Count: 1, Ms: 1800}}
		r.Releases = []release.Release{{ID: "ia", Source: "internet_archive", Name: "Second Film (2010) [h.264 720p]", Seeders: -1, Score: 90}}
	case "prowlarr":
		r.Sources = []release.SourceStatus{{Name: "prowlarr", Status: "ok", Count: 2, Ms: 41000}}
		r.Releases = []release.Release{
			{ID: "px", Source: "prowlarr", Name: "Second.Film.2010.1080p.x264", Seeders: 40, Score: 110},
			{ID: "ia", Source: "prowlarr", Name: "Second Film (2010) [h.264 720p]", Seeders: 3, Score: 70}, // same torrent, lower score
		}
	default:
		panic("progressive search must query sources one by one, got " + q.Source)
	}
	return r, nil
}

func TestProgressiveResultsMergeBySource(t *testing.T) {
	b := &twoSourceBackend{}
	var m tea.Model = tui.New(b)
	m = run(m, m.Init(), 3) // loads the status, including the release sources
	m, _ = m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	for _, k := range text("second") {
		m, _ = m.Update(k)
	}
	m = send(t, m, key(tea.KeyEnter))
	m = send(t, m, key(tea.KeyDown))
	m = send(t, m, key(tea.KeyEnter)) // open the film: both sources are searched in the background
	m = send(t, m, tea.KeyPressMsg{Code: 'd', Text: "d"})

	if len(b.releaseQueries) != 2 {
		t.Fatalf("want one request per configured source, got %+v", b.releaseQueries)
	}
	v := plain(m.View().Content)
	for _, want := range []string{"Internet Archive: 1 (1.8 s)", "Prowlarr: 2 (41.0 s)", "extra: not enabled", "1080p"} {
		if !strings.Contains(v, want) {
			t.Fatalf("merged view missing %q:\n%s", want, v)
		}
	}
	// The torrent both sources returned is listed once (the 1080p release is
	// highlighted, so the 720p one appears only in the table).
	if n := strings.Count(v, "Second Film (2010) [h.264 720p]"); n != 1 {
		t.Fatalf("duplicate torrent shown %d times:\n%s", n, v)
	}
}
