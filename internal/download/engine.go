package download

import (
	"context"

	"marquee/internal/release"
)

// Engine adds torrents and exposes them as handles.
type Engine interface {
	Add(ctx context.Context, r release.Release) (Handle, error)
	// RemoveData deletes a dropped torrent's data folder.
	RemoveData(infoHash string) error
	Close()
}

// Handle is one torrent in the engine.
type Handle interface {
	InfoHash() string
	Name() string
	WaitInfo(ctx context.Context) error
	Files() []TorrentFile
	Select(selected map[int]bool)
	FileBytes(index int) int64
	Stats() Stats
	Pause()
	Resume()
	Drop() // safe to call more than once
	// LocalPath returns where a file's data is stored on disk.
	LocalPath(index int) (string, error)
}

// Stats are live transfer figures.
type Stats struct {
	Peers   int
	Seeders int
}
