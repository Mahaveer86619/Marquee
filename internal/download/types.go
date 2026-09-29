// Package download runs torrent downloads: it adds a chosen release to the
// torrent engine, downloads only the files the requested scope needs (a film,
// an episode, a season or a whole series, with the chosen subtitle and audio
// files), reports progress, and hands finished files to the finalizer, which
// moves them into the library.
package download

import (
	"time"

	"marquee/internal/release"
)

// State is the lifecycle of a download.
type State string

const (
	Queued      State = "queued"
	Metadata    State = "metadata" // waiting for the torrent's file list
	Downloading State = "downloading"
	Paused      State = "paused"
	Finalizing  State = "finalizing" // moving files into the library
	Completed   State = "completed"
	Failed      State = "failed"
	Cancelled   State = "cancelled"
)

// Final reports whether the state is terminal.
func (s State) Final() bool { return s == Completed || s == Failed || s == Cancelled }

// Request asks for a release to be downloaded.
type Request struct {
	Release   release.Release
	Target    release.Target
	TitleRef  string   // film or series reference
	Audio     []string // ISO 639-1 codes to keep; ["*"] keeps every track
	Subtitles []string // ISO 639-1 codes to keep
}

// Download is a queued or finished download.
type Download struct {
	ID          string          `json:"id"`
	TitleRef    string          `json:"title_ref"`
	TargetKey   string          `json:"target_key"`
	Scope       release.Scope   `json:"scope"`
	Season      int             `json:"season,omitempty"`
	Episode     int             `json:"episode,omitempty"`
	ReleaseID   string          `json:"release_id"`
	Release     release.Release `json:"release"`
	Name        string          `json:"name"`
	InfoHash    string          `json:"info_hash,omitempty"`
	State       State           `json:"state"`
	Audio       []string        `json:"audio"`
	Subtitles   []string        `json:"subtitles"`
	BytesDone   int64           `json:"bytes_done"`
	BytesTotal  int64           `json:"bytes_total"`
	Error       string          `json:"error,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
}

// File is one file of a download's torrent.
type File struct {
	Index       int    `json:"index"`
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	Kind        string `json:"kind"` // video, subtitle, audio, other
	Language    string `json:"language,omitempty"`
	ItemRef     string `json:"item_ref,omitempty"` // film or episode it belongs to
	Selected    bool   `json:"selected"`
	BytesDone   int64  `json:"bytes_done"`
	LibraryPath string `json:"library_path,omitempty"`
}

// View is a download with its files and live transfer figures.
type View struct {
	Download
	Files      []File  `json:"files"`
	Progress   float64 `json:"progress"` // 0..1 of the selected files
	RateBps    int64   `json:"rate_bps"`
	ETASeconds int64   `json:"eta_seconds"`
	Peers      int     `json:"peers"`
	Seeders    int     `json:"seeders"`
}
