package download_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"marquee/internal/catalog"
	"marquee/internal/download"
	"marquee/internal/release"
	"marquee/internal/store"
)

// fakeEngine hands out fakeHandles whose files the test completes by hand.
type fakeEngine struct {
	dir     string
	files   []download.TorrentFile
	mu      sync.Mutex
	handles []*fakeHandle
	removed []string
	added   chan *fakeHandle
}

func newFakeEngine(t *testing.T, files []download.TorrentFile) *fakeEngine {
	return &fakeEngine{dir: t.TempDir(), files: files, added: make(chan *fakeHandle, 10)}
}

func (e *fakeEngine) Add(_ context.Context, r release.Release) (download.Handle, error) {
	if r.Name == "broken" {
		return nil, errors.New("tracker unreachable")
	}
	h := &fakeHandle{e: e, hash: strings.Repeat("ab", 20), bytes: map[int]int64{}}
	e.mu.Lock()
	e.handles = append(e.handles, h)
	e.mu.Unlock()
	e.added <- h
	return h, nil
}

func (e *fakeEngine) RemoveData(hash string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.removed = append(e.removed, hash)
	return nil
}

func (e *fakeEngine) Close() {}

func (e *fakeEngine) removedCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.removed)
}

type fakeHandle struct {
	e      *fakeEngine
	hash   string
	mu     sync.Mutex
	bytes  map[int]int64
	sel    map[int]bool
	paused bool
	drops  int
}

func (h *fakeHandle) InfoHash() string               { return h.hash }
func (h *fakeHandle) Name() string                   { return "Night.of.the.Living.Dead.1968" }
func (h *fakeHandle) WaitInfo(context.Context) error { return nil }
func (h *fakeHandle) Files() []download.TorrentFile  { return h.e.files }
func (h *fakeHandle) Stats() download.Stats          { return download.Stats{Peers: 3, Seeders: 1} }
func (h *fakeHandle) LocalPath(i int) (string, error) {
	return filepath.Join(h.e.dir, h.e.files[i].Path), nil
}
func (h *fakeHandle) Select(selected map[int]bool) { h.mu.Lock(); h.sel = selected; h.mu.Unlock() }
func (h *fakeHandle) Pause()                       { h.mu.Lock(); h.paused = true; h.mu.Unlock() }
func (h *fakeHandle) Resume()                      { h.mu.Lock(); h.paused = false; h.mu.Unlock() }
func (h *fakeHandle) Drop()                        { h.mu.Lock(); h.drops++; h.mu.Unlock() }
func (h *fakeHandle) isPaused() bool               { h.mu.Lock(); defer h.mu.Unlock(); return h.paused }
func (h *fakeHandle) selected() map[int]bool       { h.mu.Lock(); defer h.mu.Unlock(); return h.sel }
func (h *fakeHandle) setBytes(i int, n int64)      { h.mu.Lock(); h.bytes[i] = n; h.mu.Unlock() }
func (h *fakeHandle) FileBytes(i int) int64        { h.mu.Lock(); defer h.mu.Unlock(); return h.bytes[i] }

// finish writes the selected files to disk and marks them complete.
func (h *fakeHandle) finish(t *testing.T) {
	t.Helper()
	for i := range h.selected() {
		f := h.e.files[i]
		p := filepath.Join(h.e.dir, f.Path)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, f.Size), 0o644); err != nil {
			t.Fatal(err)
		}
		h.setBytes(i, f.Size)
	}
}

type fakeCatalog struct{}

func (fakeCatalog) Title(_ context.Context, ref string) (catalog.Title, error) {
	return catalog.Title{Ref: ref, Kind: catalog.Movie, Name: "Night of the Living Dead", Year: 1968}, nil
}

func (fakeCatalog) Episodes(context.Context, string, int) ([]catalog.Episode, error) { return nil, nil }

var filmFiles = []download.TorrentFile{
	{Index: 0, Path: "night/Night.1968.mp4", Size: 4000},
	{Index: 1, Path: "night/Night.1968.en.srt", Size: 100},
	{Index: 2, Path: "night/Night.1968.ogv", Size: 3000},
	{Index: 3, Path: "night/cover.jpg", Size: 50},
}

type env struct {
	m       *download.Manager
	st      *store.Store
	eng     *fakeEngine
	library string
}

func setup(t *testing.T, files []download.TorrentFile, maxActive int) env {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e := env{st: st, eng: newFakeEngine(t, files), library: t.TempDir()}
	e.m = e.start(t, maxActive)
	return e
}

func (e env) start(t *testing.T, maxActive int) *download.Manager {
	t.Helper()
	m := download.NewManager(download.Options{
		Engine: e.eng, Store: e.st, Catalog: fakeCatalog{}, LibraryDir: e.library,
		MaxActive: maxActive, NewID: store.NewDownloadID, Tick: 10 * time.Millisecond, SaveEvery: 1,
	})
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m
}

func request(name string) download.Request {
	return download.Request{
		Release:   release.Release{ID: "rel-" + name, Name: name, Source: "internet_archive", PrimaryFile: "night/Night.1968.mp4"},
		Target:    release.Target{Ref: "tmdb:movie:10331", Scope: release.ScopeMovie},
		TitleRef:  "tmdb:movie:10331",
		Audio:     []string{"*"},
		Subtitles: []string{"en"},
	}
}

func waitState(t *testing.T, m *download.Manager, id string, want download.State) download.View {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		v, err := m.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if v.State == want {
			return v
		}
		if time.Now().After(deadline) {
			t.Fatalf("state = %s (%s), want %s", v.State, v.Error, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func nextHandle(t *testing.T, e *fakeEngine) *fakeHandle {
	t.Helper()
	select {
	case h := <-e.added:
		return h
	case <-time.After(5 * time.Second):
		t.Fatal("torrent was never added")
		return nil
	}
}

func TestDownloadMovesFilesIntoLibrary(t *testing.T) {
	e := setup(t, filmFiles, 2)
	ctx := context.Background()
	d, err := e.m.Enqueue(ctx, request("night"))
	if err != nil {
		t.Fatal(err)
	}
	h := nextHandle(t, e.eng)
	waitState(t, e.m, d.ID, download.Downloading)
	if sel := h.selected(); len(sel) != 2 || !sel[0] || !sel[1] {
		t.Fatalf("selected = %v, want the primary video and the English subtitle", sel)
	}

	h.setBytes(0, 1000)
	deadline := time.Now().Add(5 * time.Second)
	for {
		v, _ := e.m.Get(ctx, d.ID)
		if v.BytesDone == 1000 && v.BytesTotal == 4100 && v.Peers == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("progress not reported: %+v", v)
		}
		time.Sleep(5 * time.Millisecond)
	}

	h.finish(t)
	v := waitState(t, e.m, d.ID, download.Completed)
	if v.Progress != 1 || v.CompletedAt == nil {
		t.Fatalf("completed view = %+v", v)
	}
	want := map[int]string{
		0: "Movies/Night of the Living Dead (1968)/Night of the Living Dead (1968).mp4",
		1: "Movies/Night of the Living Dead (1968)/Night of the Living Dead (1968).en.srt",
	}
	for _, f := range v.Files {
		if f.LibraryPath != want[f.Index] {
			t.Fatalf("file %d library path = %q, want %q", f.Index, f.LibraryPath, want[f.Index])
		}
		if f.LibraryPath == "" {
			continue
		}
		if st, err := os.Stat(filepath.Join(e.library, filepath.FromSlash(f.LibraryPath))); err != nil || st.Size() != f.Size {
			t.Fatalf("library file %s: %v", f.LibraryPath, err)
		}
	}
	if _, err := os.Stat(filepath.Join(e.eng.dir, "night/Night.1968.mp4")); !os.IsNotExist(err) {
		t.Fatalf("download copy should be gone, stat err = %v", err)
	}
	if e.eng.removedCount() != 1 {
		t.Fatalf("torrent data should be removed once, got %d", e.eng.removedCount())
	}
}

func TestPauseResumeAndCancel(t *testing.T) {
	e := setup(t, filmFiles, 2)
	ctx := context.Background()
	d, _ := e.m.Enqueue(ctx, request("night"))
	h := nextHandle(t, e.eng)
	waitState(t, e.m, d.ID, download.Downloading)

	if err := e.m.Pause(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if v := waitState(t, e.m, d.ID, download.Paused); v.RateBps != 0 || !h.isPaused() {
		t.Fatalf("paused view = %+v, handle paused = %v", v, h.isPaused())
	}
	if err := e.m.Resume(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	waitState(t, e.m, d.ID, download.Downloading)
	if h.isPaused() {
		t.Fatal("handle still paused after resume")
	}

	if err := e.m.Cancel(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	waitState(t, e.m, d.ID, download.Cancelled)
	if e.eng.removedCount() != 1 {
		t.Fatal("cancel should delete the partial data")
	}
	if err := e.m.Pause(ctx, d.ID); !errors.Is(err, download.ErrInvalidState) {
		t.Fatalf("pause after cancel: err = %v", err)
	}
}

func TestQueueRespectsMaxActiveAndDedupes(t *testing.T) {
	e := setup(t, filmFiles, 1)
	ctx := context.Background()
	first, _ := e.m.Enqueue(ctx, request("first"))
	again, _ := e.m.Enqueue(ctx, request("first"))
	if again.ID != first.ID {
		t.Fatal("the same release was queued twice")
	}
	second, _ := e.m.Enqueue(ctx, request("second"))

	h := nextHandle(t, e.eng)
	waitState(t, e.m, first.ID, download.Downloading)
	time.Sleep(50 * time.Millisecond)
	if v, _ := e.m.Get(ctx, second.ID); v.State != download.Queued {
		t.Fatalf("second download should wait, state = %s", v.State)
	}
	h.finish(t)
	waitState(t, e.m, first.ID, download.Completed)
	nextHandle(t, e.eng)
	waitState(t, e.m, second.ID, download.Downloading)
}

func TestFailuresAndRetry(t *testing.T) {
	e := setup(t, []download.TorrentFile{{Index: 0, Path: "readme.txt", Size: 10}}, 2)
	ctx := context.Background()

	broken, _ := e.m.Enqueue(ctx, request("broken"))
	if v := waitState(t, e.m, broken.ID, download.Failed); !strings.Contains(v.Error, "tracker unreachable") {
		t.Fatalf("error = %q", v.Error)
	}

	novideo, _ := e.m.Enqueue(ctx, request("novideo"))
	nextHandle(t, e.eng)
	if v := waitState(t, e.m, novideo.ID, download.Failed); !strings.Contains(v.Error, "no video") {
		t.Fatalf("error = %q", v.Error)
	}
	if err := e.m.Resume(ctx, novideo.ID); err != nil { // retry
		t.Fatal(err)
	}
	nextHandle(t, e.eng)
	waitState(t, e.m, novideo.ID, download.Failed)
}

func TestUnfinishedDownloadsResumeAfterRestart(t *testing.T) {
	e := setup(t, filmFiles, 2)
	ctx := context.Background()
	d, _ := e.m.Enqueue(ctx, request("night"))
	nextHandle(t, e.eng)
	waitState(t, e.m, d.ID, download.Downloading)
	e.m.Close() // core stops mid-download

	if v, _, _ := e.st.GetDownload(ctx, d.ID); v.State != download.Downloading || v.InfoHash == "" {
		t.Fatalf("stored after stop: %+v", v)
	}
	m2 := e.start(t, 2)
	h := nextHandle(t, e.eng)
	waitState(t, m2, d.ID, download.Downloading)
	if sel := h.selected(); len(sel) != 2 {
		t.Fatalf("stored selection not reused: %v", sel)
	}
	h.finish(t)
	waitState(t, m2, d.ID, download.Completed)
}
