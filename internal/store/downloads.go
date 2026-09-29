package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"marquee/internal/download"
	"marquee/internal/release"
	"marquee/internal/store/db"
)

// NewDownloadID returns an id for a new download.
func NewDownloadID() string { return newID() }

// InsertDownload stores a new download.
func (s *Store) InsertDownload(ctx context.Context, d download.Download) error {
	rel, err := json.Marshal(d.Release)
	if err != nil {
		return err
	}
	audio, _ := json.Marshal(nonNil(d.Audio))
	subs, _ := json.Marshal(nonNil(d.Subtitles))
	return s.q.InsertDownload(ctx, db.InsertDownloadParams{
		ID:        d.ID,
		TitleRef:  d.TitleRef,
		TargetKey: d.TargetKey,
		Scope:     string(d.Scope),
		Season:    optInt(d.Season),
		Episode:   optInt(d.Episode),
		ReleaseID: d.ReleaseID,
		Release:   string(rel),
		Name:      d.Name,
		InfoHash:  optStr(d.InfoHash),
		State:     string(d.State),
		Audio:     string(audio),
		Subtitles: string(subs),
		CreatedAt: d.CreatedAt.UnixMilli(),
		UpdatedAt: d.UpdatedAt.UnixMilli(),
	})
}

// GetDownload returns a download by id.
func (s *Store) GetDownload(ctx context.Context, id string) (download.Download, bool, error) {
	row, err := s.q.GetDownload(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return download.Download{}, false, nil
	}
	if err != nil {
		return download.Download{}, false, err
	}
	d, err := toDownload(row)
	return d, err == nil, err
}

// ListDownloads returns the newest downloads first.
func (s *Store) ListDownloads(ctx context.Context, limit int) ([]download.Download, error) {
	rows, err := s.q.ListDownloads(ctx, int64(limit))
	if err != nil {
		return nil, err
	}
	return toDownloads(rows)
}

// ListUnfinishedDownloads returns downloads to resume, oldest first.
func (s *Store) ListUnfinishedDownloads(ctx context.Context) ([]download.Download, error) {
	rows, err := s.q.ListUnfinishedDownloads(ctx)
	if err != nil {
		return nil, err
	}
	return toDownloads(rows)
}

// SetDownloadState changes a download's state and error message.
func (s *Store) SetDownloadState(ctx context.Context, id string, state download.State, msg string) error {
	return s.q.SetDownloadState(ctx, db.SetDownloadStateParams{
		State: string(state), Error: optStr(msg), UpdatedAt: time.Now().UnixMilli(), ID: id,
	})
}

// SetDownloadInfo records what the torrent metadata says.
func (s *Store) SetDownloadInfo(ctx context.Context, id, infoHash, name string, total int64) error {
	return s.q.SetDownloadInfo(ctx, db.SetDownloadInfoParams{
		InfoHash: optStr(infoHash), Name: name, BytesTotal: total, UpdatedAt: time.Now().UnixMilli(), ID: id,
	})
}

// SetDownloadProgress records the bytes done of the selected files.
func (s *Store) SetDownloadProgress(ctx context.Context, id string, done, total int64) error {
	return s.q.SetDownloadProgress(ctx, db.SetDownloadProgressParams{
		BytesDone: done, BytesTotal: total, UpdatedAt: time.Now().UnixMilli(), ID: id,
	})
}

// CompleteDownload marks a download completed.
func (s *Store) CompleteDownload(ctx context.Context, id string) error {
	now := time.Now().UnixMilli()
	return s.q.CompleteDownload(ctx, db.CompleteDownloadParams{CompletedAt: &now, UpdatedAt: now, ID: id})
}

// ReplaceDownloadFiles stores the file list chosen from a torrent.
func (s *Store) ReplaceDownloadFiles(ctx context.Context, id string, files []download.File) error {
	return s.inTx(ctx, func(q *db.Queries) error {
		if err := q.DeleteDownloadFiles(ctx, id); err != nil {
			return err
		}
		for _, f := range files {
			selected := int64(0)
			if f.Selected {
				selected = 1
			}
			if err := q.InsertDownloadFile(ctx, db.InsertDownloadFileParams{
				DownloadID: id,
				FileIndex:  int64(f.Index),
				Path:       f.Path,
				SizeBytes:  f.Size,
				Kind:       f.Kind,
				Language:   optStr(f.Language),
				ItemRef:    optStr(f.ItemRef),
				Selected:   selected,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListDownloadFiles returns a download's files in torrent order.
func (s *Store) ListDownloadFiles(ctx context.Context, id string) ([]download.File, error) {
	rows, err := s.q.ListDownloadFiles(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]download.File, len(rows))
	for i, r := range rows {
		out[i] = download.File{
			Index:       int(r.FileIndex),
			Path:        r.Path,
			Size:        r.SizeBytes,
			Kind:        r.Kind,
			Language:    deref(r.Language),
			ItemRef:     deref(r.ItemRef),
			Selected:    r.Selected == 1,
			BytesDone:   r.BytesDone,
			LibraryPath: deref(r.LibraryPath),
		}
	}
	return out, nil
}

// SetDownloadFileProgress records one file's bytes done.
func (s *Store) SetDownloadFileProgress(ctx context.Context, id string, index int, done int64) error {
	return s.q.SetDownloadFileProgress(ctx, db.SetDownloadFileProgressParams{BytesDone: done, DownloadID: id, FileIndex: int64(index)})
}

// SetDownloadFileLibraryPath records where a finished file was placed.
func (s *Store) SetDownloadFileLibraryPath(ctx context.Context, id string, index int, p string) error {
	return s.q.SetDownloadFileLibraryPath(ctx, db.SetDownloadFileLibraryPathParams{LibraryPath: optStr(p), DownloadID: id, FileIndex: int64(index)})
}

func toDownloads(rows []db.Download) ([]download.Download, error) {
	out := make([]download.Download, 0, len(rows))
	for _, r := range rows {
		d, err := toDownload(r)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func toDownload(r db.Download) (download.Download, error) {
	d := download.Download{
		ID:         r.ID,
		TitleRef:   r.TitleRef,
		TargetKey:  r.TargetKey,
		Scope:      release.Scope(r.Scope),
		Season:     int(derefInt(r.Season)),
		Episode:    int(derefInt(r.Episode)),
		ReleaseID:  r.ReleaseID,
		Name:       r.Name,
		InfoHash:   deref(r.InfoHash),
		State:      download.State(r.State),
		BytesDone:  r.BytesDone,
		BytesTotal: r.BytesTotal,
		Error:      deref(r.Error),
		CreatedAt:  time.UnixMilli(r.CreatedAt),
		UpdatedAt:  time.UnixMilli(r.UpdatedAt),
	}
	if r.CompletedAt != nil {
		t := time.UnixMilli(*r.CompletedAt)
		d.CompletedAt = &t
	}
	if err := json.Unmarshal([]byte(r.Release), &d.Release); err != nil {
		return d, err
	}
	if err := json.Unmarshal([]byte(r.Audio), &d.Audio); err != nil {
		return d, err
	}
	if err := json.Unmarshal([]byte(r.Subtitles), &d.Subtitles); err != nil {
		return d, err
	}
	return d, nil
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefInt(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
