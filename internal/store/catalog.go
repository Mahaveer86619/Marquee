package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"marquee/internal/catalog"
	"marquee/internal/store/db"
)

// SaveTitle inserts or updates a title and its seasons. Films also get their
// single playable item.
func (s *Store) SaveTitle(ctx context.Context, t catalog.Title) error {
	genres, err := json.Marshal(nonNil(t.Genres))
	if err != nil {
		return err
	}
	details, err := json.Marshal(t.Details)
	if err != nil {
		return err
	}
	fetched := t.FetchedAt
	if fetched.IsZero() {
		fetched = time.Now()
	}

	return s.inTx(ctx, func(q *db.Queries) error {
		titleID, err := q.UpsertTitle(ctx, db.UpsertTitleParams{
			ID:           newID(),
			Ref:          t.Ref,
			Kind:         string(t.Kind),
			Source:       t.Source,
			TmdbID:       optInt(t.TMDBID),
			TvmazeID:     optInt(t.TVmazeID),
			TvdbID:       optInt(t.TVDBID),
			ImdbID:       optStr(t.IMDbID),
			Name:         t.Name,
			OriginalName: optStr(t.OriginalName),
			Year:         optInt(t.Year),
			Overview:     optStr(t.Overview),
			Status:       optStr(t.Status),
			RuntimeMin:   optInt(t.RuntimeMin),
			Genres:       string(genres),
			PosterUrl:    optStr(t.PosterURL),
			Details:      string(details),
			FetchedAt:    fetched.UnixMilli(),
		})
		if err != nil {
			return err
		}

		for _, se := range t.Seasons {
			if err := q.UpsertSeason(ctx, db.UpsertSeasonParams{
				TitleID:      titleID,
				SeasonNumber: int64(se.Number),
				Name:         optStr(se.Name),
				AirDate:      optStr(se.AirDate),
				EpisodeCount: optInt(se.EpisodeCount),
			}); err != nil {
				return err
			}
		}

		if t.Kind == catalog.Movie {
			return q.UpsertMovieItem(ctx, db.UpsertMovieItemParams{
				ID:         newID(),
				TitleID:    titleID,
				Name:       optStr(t.Name),
				Overview:   optStr(t.Overview),
				RuntimeMin: optInt(t.RuntimeMin),
				Ref:        t.Ref,
			})
		}
		return nil
	})
}

// GetTitle returns a cached title with its seasons.
func (s *Store) GetTitle(ctx context.Context, ref string) (catalog.Title, bool, error) {
	row, err := s.q.GetTitleByRef(ctx, ref)
	if errors.Is(err, sql.ErrNoRows) {
		return catalog.Title{}, false, nil
	}
	if err != nil {
		return catalog.Title{}, false, err
	}

	t := catalog.Title{
		Ref:          row.Ref,
		Kind:         catalog.Kind(row.Kind),
		Source:       row.Source,
		Name:         row.Name,
		OriginalName: str(row.OriginalName),
		Year:         num(row.Year),
		Overview:     str(row.Overview),
		Status:       str(row.Status),
		RuntimeMin:   num(row.RuntimeMin),
		PosterURL:    str(row.PosterUrl),
		TMDBID:       num(row.TmdbID),
		TVmazeID:     num(row.TvmazeID),
		TVDBID:       num(row.TvdbID),
		IMDbID:       str(row.ImdbID),
		FetchedAt:    time.UnixMilli(row.FetchedAt),
	}
	if err := json.Unmarshal([]byte(row.Genres), &t.Genres); err != nil {
		return catalog.Title{}, false, err
	}
	if err := json.Unmarshal([]byte(row.Details), &t.Details); err != nil {
		return catalog.Title{}, false, err
	}

	seasons, err := s.q.ListSeasons(ctx, row.ID)
	if err != nil {
		return catalog.Title{}, false, err
	}
	for _, se := range seasons {
		t.Seasons = append(t.Seasons, catalog.Season{
			Number:       int(se.SeasonNumber),
			Name:         str(se.Name),
			AirDate:      str(se.AirDate),
			EpisodeCount: num(se.EpisodeCount),
		})
	}
	return t, true, nil
}

// SaveEpisodes stores the episodes of one season of a cached series.
func (s *Store) SaveEpisodes(ctx context.Context, seriesRef string, season int, episodes []catalog.Episode) error {
	return s.inTx(ctx, func(q *db.Queries) error {
		titleID, err := q.GetTitleIDByRef(ctx, seriesRef)
		if err != nil {
			return err
		}
		for _, e := range episodes {
			if err := q.UpsertEpisode(ctx, db.UpsertEpisodeParams{
				ID:            newID(),
				TitleID:       titleID,
				SeasonNumber:  optInt64(e.Season),
				EpisodeNumber: optInt64(e.Number),
				Name:          optStr(e.Name),
				Overview:      optStr(e.Overview),
				AirDate:       optStr(e.AirDate),
				RuntimeMin:    optInt(e.RuntimeMin),
				Rating:        optFloat(e.Rating),
				Ref:           e.Ref,
			}); err != nil {
				return err
			}
		}
		now := time.Now().UnixMilli()
		return q.MarkSeasonEpisodesFetched(ctx, db.MarkSeasonEpisodesFetchedParams{
			TitleID:           titleID,
			SeasonNumber:      int64(season),
			EpisodeCount:      optInt(len(episodes)),
			EpisodesFetchedAt: &now,
		})
	})
}

// GetEpisodes returns the cached episodes of a season and when they were
// fetched. found is false when the season's episodes were never fetched.
func (s *Store) GetEpisodes(ctx context.Context, seriesRef string, season int) ([]catalog.Episode, time.Time, bool, error) {
	fetched, err := s.q.GetSeasonEpisodesFetchedAt(ctx, db.GetSeasonEpisodesFetchedAtParams{
		Ref: seriesRef, SeasonNumber: int64(season),
	})
	if errors.Is(err, sql.ErrNoRows) || (err == nil && fetched == nil) {
		return nil, time.Time{}, false, nil
	}
	if err != nil {
		return nil, time.Time{}, false, err
	}

	rows, err := s.q.ListEpisodes(ctx, db.ListEpisodesParams{Ref: seriesRef, SeasonNumber: optInt64(season)})
	if err != nil {
		return nil, time.Time{}, false, err
	}
	out := make([]catalog.Episode, 0, len(rows))
	for _, r := range rows {
		out = append(out, catalog.Episode{
			Ref:        r.Ref,
			Season:     num(r.SeasonNumber),
			Number:     num(r.EpisodeNumber),
			Name:       str(r.Name),
			Overview:   str(r.Overview),
			AirDate:    str(r.AirDate),
			RuntimeMin: num(r.RuntimeMin),
			Rating:     flt(r.Rating),
		})
	}
	return out, time.UnixMilli(*fetched), true, nil
}

// ---------------------------------------------------------------------------
// Conversions between Go zero values and nullable columns.

func newID() string { return uuid.Must(uuid.NewV7()).String() }

// optInt maps 0 to NULL (used for optional numbers such as IDs and years).
func optInt(v int) *int64 {
	if v == 0 {
		return nil
	}
	n := int64(v)
	return &n
}

// optInt64 always stores the value, including 0 (e.g. season 0 = specials).
func optInt64(v int) *int64 {
	n := int64(v)
	return &n
}

func optStr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func optFloat(v float64) *float64 {
	if v == 0 {
		return nil
	}
	return &v
}

func num(v *int64) int {
	if v == nil {
		return 0
	}
	return int(*v)
}

func str(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func flt(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
