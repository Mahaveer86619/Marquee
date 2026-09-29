package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"marquee/internal/release"
	"marquee/internal/store/db"
)

// ReplaceReleases stores the results of a release search, replacing earlier
// results for the same target.
func (s *Store) ReplaceReleases(ctx context.Context, targetKey string, releases []release.Release, fetched time.Time) error {
	return s.inTx(ctx, func(q *db.Queries) error {
		if err := q.DeleteReleaseCandidates(ctx, targetKey); err != nil {
			return err
		}
		for _, r := range releases {
			if err := insertRelease(ctx, q, targetKey, r, fetched); err != nil {
				return err
			}
		}
		return nil
	})
}

// SaveRelease adds one release (e.g. a pasted magnet link) to a target.
func (s *Store) SaveRelease(ctx context.Context, targetKey string, r release.Release) error {
	return insertRelease(ctx, s.q, targetKey, r, time.Now())
}

func insertRelease(ctx context.Context, q *db.Queries, targetKey string, r release.Release, fetched time.Time) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return q.InsertReleaseCandidate(ctx, db.InsertReleaseCandidateParams{
		ID:        r.ID,
		TargetKey: targetKey,
		Source:    r.Source,
		Name:      r.Name,
		InfoHash:  optStr(r.InfoHash),
		Score:     int64(r.Score),
		Seeders:   int64(r.Seeders),
		SizeBytes: optInt64Size(r.SizeBytes),
		Data:      string(data),
		FetchedAt: fetched.UnixMilli(),
	})
}

// ListReleases returns stored results for a target, best first, and when they
// were fetched (zero when there are none).
func (s *Store) ListReleases(ctx context.Context, targetKey string) ([]release.Release, time.Time, error) {
	rows, err := s.q.ListReleaseCandidates(ctx, targetKey)
	if err != nil {
		return nil, time.Time{}, err
	}
	out := make([]release.Release, 0, len(rows))
	var fetched time.Time
	for _, row := range rows {
		var r release.Release
		if err := json.Unmarshal([]byte(row.Data), &r); err != nil {
			return nil, time.Time{}, err
		}
		out = append(out, r)
		fetched = time.UnixMilli(row.FetchedAt)
	}
	return out, fetched, nil
}

// GetRelease returns a stored release by id.
func (s *Store) GetRelease(ctx context.Context, id string) (release.Release, bool, error) {
	data, err := s.q.GetReleaseCandidate(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return release.Release{}, false, nil
	}
	if err != nil {
		return release.Release{}, false, err
	}
	var r release.Release
	return r, true, json.Unmarshal([]byte(data), &r)
}

func optInt64Size(v int64) *int64 {
	if v <= 0 {
		return nil
	}
	return &v
}
