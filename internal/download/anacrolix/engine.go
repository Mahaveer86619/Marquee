// Package anacrolix implements download.Engine with
// github.com/anacrolix/torrent.
package anacrolix

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	g "github.com/anacrolix/generics"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"github.com/anacrolix/torrent/types"

	"marquee/internal/download"
	"marquee/internal/httpx"
	"marquee/internal/release"
)

// Engine is the download.Engine backed by github.com/anacrolix/torrent.
type Engine struct {
	client *torrent.Client
	dir    string
	http   *http.Client
}

// New starts a torrent client storing data under dir (one
// folder per info hash) and listening on port.
func New(dir string, port int, log *slog.Logger) (*Engine, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = dir
	cfg.ListenPort = port
	cfg.Seed = false // finished files move into the library, so seeding stops
	if log != nil {
		cfg.Slogger = log
	}
	cfg.DefaultStorage = storage.NewFileOpts(storage.NewFileClientOpts{
		ClientBaseDir: dir,
		TorrentDirMaker: func(base string, _ *metainfo.Info, ih metainfo.Hash) string {
			return filepath.Join(base, ih.HexString())
		},
		UsePartFiles: g.Some(false),
	})
	cl, err := torrent.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("start torrent client: %w", err)
	}
	h := httpx.NewClientWithTimeout(60 * time.Second)
	h.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
		if req.URL.Scheme == "magnet" {
			return http.ErrUseLastResponse // e.g. Prowlarr redirecting a download link to a magnet
		}
		return nil
	}
	return &Engine{client: cl, dir: dir, http: h}, nil
}

// Close stops the client.
func (e *Engine) Close() { e.client.Close() }

// RemoveData deletes the data folder of a torrent. Call it after Drop.
func (e *Engine) RemoveData(infoHash string) error {
	if len(infoHash) != 40 || strings.Trim(strings.ToLower(infoHash), "0123456789abcdef") != "" {
		return fmt.Errorf("invalid info hash %q", infoHash)
	}
	return os.RemoveAll(filepath.Join(e.dir, strings.ToLower(infoHash)))
}

// Add adds a release by torrent file (preferred for web-seeded sources such as
// the Internet Archive, and when there is no magnet) or by magnet link.
func (e *Engine) Add(ctx context.Context, r release.Release) (download.Handle, error) {
	useFile := r.TorrentURL != "" && (r.Magnet == "" || r.Source == "internet_archive")
	if useFile {
		mi, magnet, err := e.fetchTorrent(ctx, r.TorrentURL)
		switch {
		case err != nil && r.Magnet == "" && r.InfoHash == "":
			return nil, err
		case err == nil && mi != nil:
			t, err := e.client.AddTorrent(mi)
			if err != nil {
				return nil, err
			}
			return &anacrolixHandle{t: t, dir: e.dir}, nil
		case magnet != "":
			r.Magnet = magnet
		}
	}
	if r.Magnet == "" && r.InfoHash != "" {
		r.Magnet = "magnet:?xt=urn:btih:" + r.InfoHash
	}
	if r.Magnet == "" {
		return nil, errors.New("release has no torrent link, magnet link or info hash")
	}
	t, err := e.client.AddMagnet(r.Magnet)
	if err != nil {
		return nil, fmt.Errorf("add magnet: %w", err)
	}
	return &anacrolixHandle{t: t, dir: e.dir}, nil
}

// fetchTorrent downloads a .torrent file. A redirect to a magnet link (as
// Prowlarr does for magnet-only indexers) is returned as magnet instead.
func (e *Engine) fetchTorrent(ctx context.Context, rawURL string) (*metainfo.MetaInfo, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "application/x-bittorrent, */*")
	req.Header.Set("User-Agent", httpx.UserAgent)
	resp, err := e.http.Do(req)
	if err != nil {
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err // the URL can carry an API key
		}
		return nil, "", fmt.Errorf("fetch torrent: %w", err)
	}
	defer resp.Body.Close()
	if loc := resp.Header.Get("Location"); strings.HasPrefix(loc, "magnet:") {
		return nil, loc, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("fetch torrent: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, "", fmt.Errorf("fetch torrent: %w", err)
	}
	if strings.HasPrefix(string(body), "magnet:") {
		return nil, strings.TrimSpace(string(body)), nil
	}
	mi, err := metainfo.Load(bytes.NewReader(body))
	if err != nil {
		return nil, "", fmt.Errorf("not a torrent file: %w", err)
	}
	return mi, "", nil
}

type anacrolixHandle struct {
	t    *torrent.Torrent
	dir  string
	drop sync.Once
}

func (h *anacrolixHandle) InfoHash() string { return h.t.InfoHash().HexString() }
func (h *anacrolixHandle) Name() string     { return h.t.Name() }

func (h *anacrolixHandle) WaitInfo(ctx context.Context) error {
	select {
	case <-h.t.GotInfo():
		return nil
	case <-ctx.Done():
		return fmt.Errorf("waiting for torrent metadata: %w", ctx.Err())
	}
}

func (h *anacrolixHandle) Files() []download.TorrentFile {
	tfs := h.t.Files()
	out := make([]download.TorrentFile, len(tfs))
	for i, f := range tfs {
		out[i] = download.TorrentFile{Index: i, Path: f.Path(), Size: f.Length()}
	}
	return out
}

func (h *anacrolixHandle) Select(selected map[int]bool) {
	for i, f := range h.t.Files() {
		if selected[i] {
			f.SetPriority(types.PiecePriorityNormal)
		} else {
			f.SetPriority(types.PiecePriorityNone)
		}
	}
}

func (h *anacrolixHandle) FileBytes(index int) int64 {
	tfs := h.t.Files()
	if index < 0 || index >= len(tfs) {
		return 0
	}
	return tfs[index].BytesCompleted()
}

func (h *anacrolixHandle) Stats() download.Stats {
	s := h.t.Stats()
	return download.Stats{Peers: s.ActivePeers, Seeders: s.ConnectedSeeders}
}

func (h *anacrolixHandle) Pause()  { h.t.DisallowDataDownload() }
func (h *anacrolixHandle) Resume() { h.t.AllowDataDownload() }
func (h *anacrolixHandle) Drop()   { h.drop.Do(h.t.Drop) }

func (h *anacrolixHandle) LocalPath(index int) (string, error) {
	tfs := h.t.Files()
	if index < 0 || index >= len(tfs) {
		return "", errors.New("no such file")
	}
	root := filepath.Join(h.dir, h.InfoHash())
	f := tfs[index]
	for _, candidate := range []string{
		filepath.Join(root, filepath.FromSlash(f.Path())),
		filepath.Join(root, filepath.FromSlash(strings.Join(f.FileInfo().PathUtf8, "/"))),
		filepath.Join(root, filepath.FromSlash(strings.Join(f.FileInfo().Path, "/"))),
	} {
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate, nil
		}
	}
	// Fall back to finding the file by name and size.
	want := filepath.Base(filepath.FromSlash(f.Path()))
	var found string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || found != "" {
			return nil
		}
		if info, err := d.Info(); err == nil && d.Name() == want && info.Size() == f.Length() {
			found = p
		}
		return nil
	})
	if found == "" {
		return "", fmt.Errorf("data for %s not found under %s", f.Path(), root)
	}
	return found, nil
}
