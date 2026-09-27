package metadata_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"marquee/internal/catalog"
	"marquee/internal/metadata"
	"marquee/internal/store"
)

// fakeProvider counts calls and can be made to fail.
type fakeProvider struct {
	name   string
	calls  atomic.Int32
	failed bool
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Search(context.Context, string) ([]catalog.SearchResult, error) {
	f.calls.Add(1)
	if f.failed {
		return nil, errors.New("provider down")
	}
	return []catalog.SearchResult{{Ref: f.name + ":show:1", Kind: catalog.Series, Name: "From " + f.name, Source: f.name}}, nil
}

func (f *fakeProvider) Title(_ context.Context, ref catalog.Ref) (catalog.Title, error) {
	f.calls.Add(1)
	if f.failed {
		return catalog.Title{}, errors.New("provider down")
	}
	return catalog.Title{Ref: ref.String(), Kind: catalog.Series, Source: f.name, Name: "Cached Show",
		Seasons: []catalog.Season{{Number: 1, EpisodeCount: 1}},
		Details: catalog.Details{Version: catalog.DetailsVersion, Rating: 8.1}}, nil
}

func (f *fakeProvider) Episodes(_ context.Context, ref catalog.Ref, season int) ([]catalog.Episode, error) {
	f.calls.Add(1)
	return []catalog.Episode{{Ref: catalog.EpisodeRef(ref.String(), season, 1), Season: season, Number: 1, Name: "Pilot"}}, nil
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

func TestSearchUsesTVmazeWithoutTMDB(t *testing.T) {
	tv := &fakeProvider{name: "tvmaze"}
	svc := metadata.NewService(openStore(t), nil, tv)

	out, err := svc.Search(context.Background(), "show")
	if err != nil || out.Source != "tvmaze" || len(out.Results) != 1 {
		t.Fatalf("outcome=%+v err=%v", out, err)
	}
	if out.Notice != "" {
		t.Fatalf("no TMDB configured, so no fallback notice expected; got %q", out.Notice)
	}
	if svc.Checks(context.Background())["tmdb"] != "not_configured" {
		t.Fatal("tmdb check should be not_configured")
	}
}

func TestSearchFallsBackWhenTMDBFails(t *testing.T) {
	var hits atomic.Int32
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer down.Close()
	tmdb := newTMDB(down.URL)
	tv := &fakeProvider{name: "tvmaze"}

	out, err := metadata.NewService(openStore(t), tmdb, tv).Search(context.Background(), "show")
	if err != nil || out.Source != "tvmaze" {
		t.Fatalf("outcome=%+v err=%v, want tvmaze fallback", out, err)
	}
	if out.Notice == "" {
		t.Fatal("fallback must explain itself with a notice")
	}
	if n := hits.Load(); n != 3 {
		t.Fatalf("TMDB called %d times, want 3 (5xx is retried twice)", n)
	}
}

func TestSearchRetrySucceeds(t *testing.T) {
	var hits atomic.Int32
	flaky := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"results":[{"id":157336,"media_type":"movie","title":"Example Film","release_date":"2014-11-05"}]}`))
	}))
	defer flaky.Close()

	out, err := metadata.NewService(openStore(t), newTMDB(flaky.URL), &fakeProvider{name: "tvmaze"}).Search(context.Background(), "film")
	if err != nil || out.Source != "tmdb" || len(out.Results) != 1 || out.Notice != "" {
		t.Fatalf("outcome=%+v err=%v, want tmdb after one retry", out, err)
	}
}

func TestTitleIsCached(t *testing.T) {
	tv := &fakeProvider{name: "tvmaze"}
	svc := metadata.NewService(openStore(t), nil, tv)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		title, err := svc.Title(ctx, "tvmaze:show:5")
		if err != nil || title.Name != "Cached Show" {
			t.Fatalf("title=%+v err=%v", title, err)
		}
	}
	if n := tv.calls.Load(); n != 1 {
		t.Fatalf("provider called %d times, want 1 (then cache)", n)
	}
}

func TestOldCachedTitlesAreRefreshed(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	// A title cached before details existed (version 0).
	if err := st.SaveTitle(ctx, catalog.Title{Ref: "tvmaze:show:7", Kind: catalog.Series, Source: "tvmaze", Name: "Old"}); err != nil {
		t.Fatal(err)
	}
	tv := &fakeProvider{name: "tvmaze"}
	title, err := metadata.NewService(st, nil, tv).Title(ctx, "tvmaze:show:7")
	if err != nil {
		t.Fatal(err)
	}
	if tv.calls.Load() != 1 || title.Details.Rating != 8.1 {
		t.Fatalf("stale title not refreshed: calls=%d title=%+v", tv.calls.Load(), title)
	}
}

func TestEpisodesAreCached(t *testing.T) {
	tv := &fakeProvider{name: "tvmaze"}
	svc := metadata.NewService(openStore(t), nil, tv)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		eps, err := svc.Episodes(ctx, "tvmaze:show:5", 1)
		if err != nil || len(eps) != 1 || eps[0].Ref != "tvmaze:show:5:s01e01" {
			t.Fatalf("eps=%+v err=%v", eps, err)
		}
	}
	// One title fetch plus one episodes fetch; the second round is served from the cache.
	if n := tv.calls.Load(); n != 2 {
		t.Fatalf("provider called %d times, want 2", n)
	}
}

func TestUnknownReference(t *testing.T) {
	svc := metadata.NewService(openStore(t), nil, &fakeProvider{name: "tvmaze"})
	if _, err := svc.Title(context.Background(), "nope"); !errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if _, err := svc.Title(context.Background(), "other:show:1"); !errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound for an unknown provider", err)
	}
}

func TestTMDBCheckStates(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer ok.Close()
	rejected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer rejected.Close()

	tv := &fakeProvider{name: "tvmaze"}
	if got := metadata.NewService(openStore(t), newTMDB(ok.URL), tv).Checks(context.Background())["tmdb"]; got != "valid" {
		t.Fatalf("check = %s, want valid", got)
	}
	if got := metadata.NewService(openStore(t), newTMDB(rejected.URL), tv).Checks(context.Background())["tmdb"]; got != "invalid" {
		t.Fatalf("check = %s, want invalid", got)
	}
}
