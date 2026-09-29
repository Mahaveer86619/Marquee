package release_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	"marquee/internal/release"
	"marquee/internal/store"
)

type fakeSource struct {
	name     string
	releases []release.Release
	err      error
	calls    atomic.Int32
	films    bool
}

func (f *fakeSource) Name() string { return f.name }

func (f *fakeSource) Supports(t release.Target) bool {
	return !f.films || t.Scope == release.ScopeMovie
}

func (f *fakeSource) Search(context.Context, release.Target) ([]release.Release, error) {
	f.calls.Add(1)
	return f.releases, f.err
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestServiceRanksFiltersDedupesAndCaches(t *testing.T) {
	good := release.Release{Source: "a", Name: "Example.Film.2014.1080p.WEB-DL.AAC.x264-GRP", InfoHash: "h1", Seeders: 50}
	worse := release.Release{Source: "a", Name: "Example.Film.2014.720p.WEB-DL.x265-GRP", InfoHash: "h2", Seeders: 50}
	other := release.Release{Source: "a", Name: "Different.Film.2014.1080p.x264", InfoHash: "h3", Seeders: 500}
	dup := release.Release{Source: "b", Name: "Example.Film.2014.1080p.WEB-DL.AAC.x264-GRP", InfoHash: "h1", Seeders: 50}

	a := &fakeSource{name: "a", releases: []release.Release{worse, good, other}}
	b := &fakeSource{name: "b", releases: []release.Release{dup}}
	down := &fakeSource{name: "down", err: errors.New("provider error 500")}
	svc := release.NewService(openStore(t), release.Prefs{}, map[string]string{"prowlarr": "not enabled"}, a, b, down)

	res, err := svc.Search(context.Background(), film(), false, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Releases) != 2 {
		t.Fatalf("got %d releases, want 2 (other title dropped, duplicate merged)", len(res.Releases))
	}
	if res.Releases[0].InfoHash != "h1" || res.Releases[0].ID == "" {
		t.Fatalf("best release = %+v", res.Releases[0])
	}
	statuses := map[string]string{}
	for _, s := range res.Sources {
		statuses[s.Name] = s.Status
	}
	if statuses["a"] != "ok" || statuses["down"] != "error" || statuses["prowlarr"] != "not_configured" {
		t.Fatalf("statuses = %v", statuses)
	}

	// The same search is served from the cache; a refresh searches again.
	again, _ := svc.Search(context.Background(), film(), false, "")
	if !again.Cached || a.calls.Load() != 1 {
		t.Fatalf("cached=%v calls=%d", again.Cached, a.calls.Load())
	}
	if _, err := svc.Search(context.Background(), film(), true, ""); err != nil || a.calls.Load() != 2 {
		t.Fatalf("refresh did not search again: calls=%d err=%v", a.calls.Load(), err)
	}

	// Ids are stable across refreshes, so a release chosen from an older list
	// can still be fetched by id for queueing.
	got, found, err := svc.Get(context.Background(), again.Releases[0].ID)
	if err != nil || !found || got.Name != again.Releases[0].Name {
		t.Fatalf("get after refresh: found=%v err=%v", found, err)
	}
	if again.Releases[0].ID != res.Releases[0].ID {
		t.Fatalf("id changed between searches: %s vs %s", res.Releases[0].ID, again.Releases[0].ID)
	}
}

func TestServiceSkipsUnsupportedSources(t *testing.T) {
	filmsOnly := &fakeSource{name: "internet_archive", films: true}
	svc := release.NewService(openStore(t), release.Prefs{}, nil, filmsOnly)
	res, err := svc.Search(context.Background(), show(release.ScopeEpisode, 1, 1), false, "")
	if err != nil {
		t.Fatal(err)
	}
	if filmsOnly.calls.Load() != 0 || res.Sources[0].Status != "not_applicable" {
		t.Fatalf("calls=%d status=%+v", filmsOnly.calls.Load(), res.Sources)
	}
}

func TestAddMagnet(t *testing.T) {
	svc := release.NewService(openStore(t), release.Prefs{}, nil)
	r, err := svc.AddMagnet(context.Background(), film(), "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Example.Film.2014.1080p.x264")
	if err != nil || r.ID == "" || r.Parsed.Resolution != "1080p" {
		t.Fatalf("release=%+v err=%v", r, err)
	}
	if _, found, _ := svc.Get(context.Background(), r.ID); !found {
		t.Fatal("magnet release not stored")
	}
}
