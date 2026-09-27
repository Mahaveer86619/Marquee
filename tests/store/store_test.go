package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"marquee/internal/catalog"
	"marquee/internal/store"
)

func open(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func series() catalog.Title {
	return catalog.Title{
		Ref: "tmdb:tv:1399", Kind: catalog.Series, Source: "tmdb", Name: "Example Series", Year: 2011,
		Genres: []string{"Drama", "Fantasy"}, TMDBID: 1399, TVDBID: 121361, IMDbID: "tt0944947",
		Seasons: []catalog.Season{
			{Number: 0, Name: "Specials", EpisodeCount: 3},
			{Number: 1, Name: "Season 1", AirDate: "2011-04-17", EpisodeCount: 10},
		},
		FetchedAt: time.Now(),
	}
}

func TestMigrationsApplyAndAreIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.db")
	for i := 0; i < 2; i++ {
		st, err := store.Open(context.Background(), path)
		if err != nil {
			t.Fatalf("open #%d: %v", i+1, err)
		}
		v, err := st.SchemaVersion(context.Background())
		if err != nil || v < 1 {
			t.Fatalf("schema version = %d, err = %v", v, err)
		}
		st.Close()
	}
}

func TestSaveAndGetSeries(t *testing.T) {
	st, ctx := open(t), context.Background()
	if err := st.SaveTitle(ctx, series()); err != nil {
		t.Fatal(err)
	}
	got, found, err := st.GetTitle(ctx, "tmdb:tv:1399")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if got.Name != "Example Series" || got.TVDBID != 121361 || len(got.Genres) != 2 || len(got.Seasons) != 2 {
		t.Fatalf("unexpected title: %+v", got)
	}
	if got.Seasons[1].EpisodeCount != 10 {
		t.Fatalf("season 1 = %+v", got.Seasons[1])
	}
}

func TestSaveTitleUpdatesInPlace(t *testing.T) {
	st, ctx := open(t), context.Background()
	s := series()
	if err := st.SaveTitle(ctx, s); err != nil {
		t.Fatal(err)
	}
	s.Name, s.Status = "Renamed", "Ended"
	if err := st.SaveTitle(ctx, s); err != nil {
		t.Fatal(err)
	}
	got, _, _ := st.GetTitle(ctx, s.Ref)
	if got.Name != "Renamed" || got.Status != "Ended" {
		t.Fatalf("update not applied: %+v", got)
	}
}

func TestMissingTitle(t *testing.T) {
	_, found, err := open(t).GetTitle(context.Background(), "tmdb:movie:1")
	if err != nil || found {
		t.Fatalf("found=%v err=%v", found, err)
	}
}

func TestSaveAndGetEpisodes(t *testing.T) {
	st, ctx := open(t), context.Background()
	if err := st.SaveTitle(ctx, series()); err != nil {
		t.Fatal(err)
	}
	if _, _, found, _ := st.GetEpisodes(ctx, "tmdb:tv:1399", 1); found {
		t.Fatal("episodes reported before they were fetched")
	}
	eps := []catalog.Episode{
		{Ref: "tmdb:tv:1399:s01e02", Season: 1, Number: 2, Name: "Second"},
		{Ref: "tmdb:tv:1399:s01e01", Season: 1, Number: 1, Name: "First", AirDate: "2011-04-17"},
	}
	if err := st.SaveEpisodes(ctx, "tmdb:tv:1399", 1, eps); err != nil {
		t.Fatal(err)
	}
	got, fetched, found, err := st.GetEpisodes(ctx, "tmdb:tv:1399", 1)
	if err != nil || !found || fetched.IsZero() {
		t.Fatalf("found=%v fetched=%v err=%v", found, fetched, err)
	}
	if len(got) != 2 || got[0].Number != 1 || got[0].Name != "First" {
		t.Fatalf("episodes not ordered or incomplete: %+v", got)
	}
}

func TestMovieGetsPlayableItem(t *testing.T) {
	st, ctx := open(t), context.Background()
	movie := catalog.Title{Ref: "tmdb:movie:603", Kind: catalog.Movie, Source: "tmdb", Name: "Example Film", Year: 1999}
	if err := st.SaveTitle(ctx, movie); err != nil {
		t.Fatal(err)
	}
	// Saving twice must not violate the one-item-per-film constraint.
	if err := st.SaveTitle(ctx, movie); err != nil {
		t.Fatal(err)
	}
}
