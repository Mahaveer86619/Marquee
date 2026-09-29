package download

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"marquee/internal/catalog"
	"marquee/internal/release"
)

// Store persists downloads. It is implemented by internal/store.
type Store interface {
	InsertDownload(ctx context.Context, d Download) error
	GetDownload(ctx context.Context, id string) (Download, bool, error)
	ListDownloads(ctx context.Context, limit int) ([]Download, error)
	ListUnfinishedDownloads(ctx context.Context) ([]Download, error)
	SetDownloadState(ctx context.Context, id string, state State, msg string) error
	SetDownloadInfo(ctx context.Context, id, infoHash, name string, total int64) error
	SetDownloadProgress(ctx context.Context, id string, done, total int64) error
	CompleteDownload(ctx context.Context, id string) error
	ReplaceDownloadFiles(ctx context.Context, id string, files []File) error
	ListDownloadFiles(ctx context.Context, id string) ([]File, error)
	SetDownloadFileProgress(ctx context.Context, id string, index int, done int64) error
	SetDownloadFileLibraryPath(ctx context.Context, id string, index int, p string) error
}

// Catalog supplies the names used for library paths.
type Catalog interface {
	Title(ctx context.Context, ref string) (catalog.Title, error)
	Episodes(ctx context.Context, ref string, season int) ([]catalog.Episode, error)
}

// Errors returned by the manager.
var (
	ErrNotFound     = errors.New("download not found")
	ErrInvalidState = errors.New("download cannot do that in its current state")
)

// Options configures a Manager.
type Options struct {
	Engine     Engine
	Store      Store
	Catalog    Catalog // optional; names fall back to the release name
	LibraryDir string  // where finished files are moved
	MaxActive  int     // downloads running at once (default 2)
	NewID      func() string
	Log        *slog.Logger
	// Tick is the progress interval (default 1 s); progress is saved every
	// SaveEvery ticks (default 5).
	Tick      time.Duration
	SaveEvery int
}

// Manager runs the download queue: it starts queued downloads up to
// MaxActive, tracks progress, supports pause, resume and cancel, moves
// finished files into the library, and resumes unfinished downloads after a
// restart.
type Manager struct {
	opts Options
	log  *slog.Logger

	mu   sync.Mutex
	jobs map[string]*job
	wake chan struct{}

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// job is a running download.
type job struct {
	id     string
	cancel context.CancelFunc
	done   chan struct{}

	mu        sync.Mutex
	handle    Handle
	paused    bool
	cancelled bool
	files     []File // with live bytes
	bytes     int64
	total     int64
	rate      float64 // bytes per second, smoothed
	stats     Stats
}

// NewManager returns a manager. Call Start to run it.
func NewManager(opts Options) *Manager {
	if opts.MaxActive <= 0 {
		opts.MaxActive = 2
	}
	if opts.Tick <= 0 {
		opts.Tick = time.Second
	}
	if opts.SaveEvery <= 0 {
		opts.SaveEvery = 5
	}
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	return &Manager{opts: opts, log: log, jobs: map[string]*job{}, wake: make(chan struct{}, 1)}
}

// Start resets downloads interrupted by a restart and runs the scheduler
// until Close.
func (m *Manager) Start(ctx context.Context) error {
	m.ctx, m.cancel = context.WithCancel(ctx)
	list, err := m.opts.Store.ListUnfinishedDownloads(ctx)
	if err != nil {
		return err
	}
	for _, d := range list {
		if d.State != Queued && d.State != Paused {
			if err := m.opts.Store.SetDownloadState(ctx, d.ID, Queued, ""); err != nil {
				return err
			}
		}
	}
	m.wg.Go(m.schedule)
	m.poke()
	return nil
}

// Close stops every running download (their state is kept, so they resume on
// the next start) and waits for them to finish.
func (m *Manager) Close() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
}

func (m *Manager) poke() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// schedule starts queued downloads whenever a slot frees up.
func (m *Manager) schedule() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		m.fill()
		select {
		case <-m.ctx.Done():
			return
		case <-m.wake:
		case <-t.C:
		}
	}
}

func (m *Manager) fill() {
	list, err := m.opts.Store.ListUnfinishedDownloads(m.ctx)
	if err != nil {
		if m.ctx.Err() == nil {
			m.log.Warn("download queue", "err", err)
		}
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	active := 0
	for _, j := range m.jobs {
		j.mu.Lock()
		if !j.paused {
			active++
		}
		j.mu.Unlock()
	}
	for _, d := range list { // oldest first
		if active >= m.opts.MaxActive || m.ctx.Err() != nil {
			return
		}
		if d.State != Queued || m.jobs[d.ID] != nil {
			continue
		}
		ctx, cancel := context.WithCancel(m.ctx)
		j := &job{id: d.ID, cancel: cancel, done: make(chan struct{})}
		m.jobs[d.ID] = j
		active++
		m.wg.Go(func() {
			defer close(j.done)
			m.run(ctx, j, d)
			m.mu.Lock()
			delete(m.jobs, d.ID)
			m.mu.Unlock()
			m.poke()
		})
	}
}

// Enqueue stores a new download and schedules it. A release that is already
// queued or running for the same target is returned instead of being added
// twice.
func (m *Manager) Enqueue(ctx context.Context, r Request) (Download, error) {
	if r.Release.ID == "" {
		return Download{}, errors.New("release is required")
	}
	if r.TitleRef == "" {
		return Download{}, errors.New("title reference is required")
	}
	list, err := m.opts.Store.ListUnfinishedDownloads(ctx)
	if err != nil {
		return Download{}, err
	}
	for _, d := range list {
		if d.ReleaseID == r.Release.ID && d.TargetKey == r.Target.Key() {
			return d, nil
		}
	}
	now := time.Now()
	d := Download{
		ID:        m.opts.NewID(),
		TitleRef:  r.TitleRef,
		TargetKey: r.Target.Key(),
		Scope:     r.Target.Scope,
		Season:    r.Target.Season,
		Episode:   r.Target.Episode,
		ReleaseID: r.Release.ID,
		Release:   r.Release,
		Name:      r.Release.Name,
		InfoHash:  r.Release.InfoHash,
		State:     Queued,
		Audio:     nonNil(r.Audio),
		Subtitles: nonNil(r.Subtitles),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := m.opts.Store.InsertDownload(ctx, d); err != nil {
		return Download{}, err
	}
	m.log.Info("download queued", "id", d.ID, "name", d.Name, "scope", d.Scope)
	m.poke()
	return d, nil
}

// List returns the newest downloads with their files and live figures.
func (m *Manager) List(ctx context.Context, limit int) ([]View, error) {
	list, err := m.opts.Store.ListDownloads(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]View, 0, len(list))
	for _, d := range list {
		v, err := m.view(ctx, d)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// Get returns one download.
func (m *Manager) Get(ctx context.Context, id string) (View, error) {
	d, found, err := m.opts.Store.GetDownload(ctx, id)
	if err != nil {
		return View{}, err
	}
	if !found {
		return View{}, ErrNotFound
	}
	return m.view(ctx, d)
}

func (m *Manager) view(ctx context.Context, d Download) (View, error) {
	v := View{Download: d}
	m.mu.Lock()
	j := m.jobs[d.ID]
	m.mu.Unlock()
	if j != nil {
		j.mu.Lock()
		if j.files != nil {
			v.Files = slices.Clone(j.files)
			v.BytesDone, v.BytesTotal = j.bytes, j.total
		}
		if !j.paused {
			v.RateBps = int64(j.rate)
		}
		v.Peers, v.Seeders = j.stats.Peers, j.stats.Seeders
		j.mu.Unlock()
	}
	if v.Files == nil {
		files, err := m.opts.Store.ListDownloadFiles(ctx, d.ID)
		if err != nil {
			return View{}, err
		}
		v.Files = files
	}
	if v.Files == nil {
		v.Files = []File{}
	}
	switch {
	case d.State == Completed:
		v.Progress = 1
	case v.BytesTotal > 0:
		v.Progress = float64(v.BytesDone) / float64(v.BytesTotal)
	}
	if v.RateBps > 0 && v.BytesTotal > v.BytesDone {
		v.ETASeconds = (v.BytesTotal - v.BytesDone) / v.RateBps
	}
	return v, nil
}

// Pause stops a queued or running download without discarding its data.
func (m *Manager) Pause(ctx context.Context, id string) error {
	d, err := m.load(ctx, id)
	if err != nil {
		return err
	}
	switch d.State {
	case Paused:
		return nil
	case Queued, Metadata, Downloading:
	default:
		return ErrInvalidState
	}
	m.mu.Lock()
	j := m.jobs[id]
	m.mu.Unlock()
	if j != nil {
		j.mu.Lock()
		j.paused = true
		if j.handle != nil {
			j.handle.Pause()
		}
		j.mu.Unlock()
	}
	if err := m.opts.Store.SetDownloadState(ctx, id, Paused, ""); err != nil {
		return err
	}
	m.poke() // a slot may have freed up
	return nil
}

// Resume continues a paused download, or retries a failed one.
func (m *Manager) Resume(ctx context.Context, id string) error {
	d, err := m.load(ctx, id)
	if err != nil {
		return err
	}
	if d.State != Paused && d.State != Failed {
		if d.State.Final() {
			return ErrInvalidState
		}
		return nil
	}
	m.mu.Lock()
	j := m.jobs[id]
	m.mu.Unlock()
	if j != nil {
		j.mu.Lock()
		j.paused = false
		state := Metadata
		if j.handle != nil && j.files != nil {
			j.handle.Resume()
			state = Downloading
		}
		j.mu.Unlock()
		return m.opts.Store.SetDownloadState(ctx, id, state, "")
	}
	if err := m.opts.Store.SetDownloadState(ctx, id, Queued, ""); err != nil {
		return err
	}
	m.poke()
	return nil
}

// Cancel stops a download and deletes its partial data. Files already moved
// into the library are kept.
func (m *Manager) Cancel(ctx context.Context, id string) error {
	d, err := m.load(ctx, id)
	if err != nil {
		return err
	}
	if d.State.Final() {
		if d.State == Cancelled {
			return nil
		}
		return ErrInvalidState
	}
	m.mu.Lock()
	j := m.jobs[id]
	m.mu.Unlock()
	if j != nil {
		j.mu.Lock()
		j.cancelled = true
		j.mu.Unlock()
		j.cancel()
		<-j.done
		if d2, _, err := m.opts.Store.GetDownload(ctx, id); err == nil && d2.InfoHash != "" {
			d.InfoHash = d2.InfoHash
		}
	}
	if d.InfoHash != "" {
		if err := m.opts.Engine.RemoveData(d.InfoHash); err != nil {
			m.log.Warn("cannot remove download data", "id", id, "err", err)
		}
	}
	m.log.Info("download cancelled", "id", id)
	return m.opts.Store.SetDownloadState(ctx, id, Cancelled, "")
}

func (m *Manager) load(ctx context.Context, id string) (Download, error) {
	d, found, err := m.opts.Store.GetDownload(ctx, id)
	if err != nil {
		return Download{}, err
	}
	if !found {
		return Download{}, ErrNotFound
	}
	return d, nil
}

// run drives one download from adding the torrent to completion.
func (m *Manager) run(ctx context.Context, j *job, d Download) {
	st := m.opts.Store
	fail := func(err error) {
		if ctx.Err() != nil {
			return // cancelled or shutting down: the caller sets the state
		}
		m.log.Warn("download failed", "id", d.ID, "name", d.Name, "err", err)
		_ = st.SetDownloadState(context.WithoutCancel(ctx), d.ID, Failed, err.Error())
	}
	setState := func(s State) {
		j.mu.Lock()
		if j.paused {
			s = Paused
		}
		j.mu.Unlock()
		_ = st.SetDownloadState(ctx, d.ID, s, "")
	}

	setState(Metadata)
	rel := d.Release
	if rel.InfoHash == "" {
		rel.InfoHash = d.InfoHash // known from an earlier run
	}
	h, err := m.opts.Engine.Add(ctx, rel)
	if err != nil {
		fail(err)
		return
	}
	defer h.Drop()
	j.mu.Lock()
	j.handle = h
	if j.paused {
		h.Pause()
	}
	j.mu.Unlock()

	if err := h.WaitInfo(ctx); err != nil {
		fail(err)
		return
	}
	files, err := m.chooseFiles(ctx, d, h)
	if err != nil {
		fail(err)
		return
	}
	selected := map[int]bool{}
	var total int64
	for _, f := range files {
		if f.Selected {
			selected[f.Index] = true
			total += f.Size
		}
	}
	if err := st.SetDownloadInfo(ctx, d.ID, h.InfoHash(), h.Name(), total); err != nil {
		fail(err)
		return
	}
	h.Select(selected)
	j.mu.Lock()
	j.files, j.total = files, total
	j.mu.Unlock()
	setState(Downloading)
	m.log.Info("download started", "id", d.ID, "name", h.Name(), "files", len(selected), "bytes", total)

	if !m.transfer(ctx, j, h) {
		return
	}

	setState(Finalizing)
	j.mu.Lock()
	files = slices.Clone(j.files)
	j.mu.Unlock()
	if err := m.finalize(ctx, d, h, files); err != nil {
		fail(fmt.Errorf("move to library: %w", err))
		return
	}
	if err := m.opts.Engine.RemoveData(h.InfoHash()); err != nil {
		m.log.Warn("cannot remove download data", "id", d.ID, "err", err)
	}
	if err := st.CompleteDownload(ctx, d.ID); err != nil {
		fail(err)
		return
	}
	m.log.Info("download completed", "id", d.ID, "name", h.Name())
}

// chooseFiles returns the stored file list when resuming, or selects the
// files for the download's scope from the torrent.
func (m *Manager) chooseFiles(ctx context.Context, d Download, h Handle) ([]File, error) {
	tfs := h.Files()
	stored, err := m.opts.Store.ListDownloadFiles(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	if len(stored) == len(tfs) && len(stored) > 0 {
		return stored, nil
	}
	t := release.Target{Scope: d.Scope, Season: d.Season, Episode: d.Episode}
	files := SelectFiles(tfs, t, d.TitleRef, d.Release.PrimaryFile, d.Audio, d.Subtitles)
	if !slices.ContainsFunc(files, func(f File) bool { return f.Selected && f.Kind == "video" }) {
		return nil, fmt.Errorf("no video in this torrent matches the requested %s", d.Scope)
	}
	if err := m.opts.Store.ReplaceDownloadFiles(ctx, d.ID, files); err != nil {
		return nil, err
	}
	return files, nil
}

// transfer waits until every selected file is complete, updating progress.
// It returns false when the context ends first.
func (m *Manager) transfer(ctx context.Context, j *job, h Handle) bool {
	tick := time.NewTicker(m.opts.Tick)
	defer tick.Stop()
	last, lastAt := int64(-1), time.Now()
	saved := map[int]int64{}
	for n := 0; ; n++ {
		j.mu.Lock()
		var done int64
		complete := true
		for i := range j.files {
			f := &j.files[i]
			if !f.Selected {
				continue
			}
			f.BytesDone = min(h.FileBytes(f.Index), f.Size)
			done += f.BytesDone
			if f.BytesDone < f.Size {
				complete = false
			}
		}
		now := time.Now()
		if last >= 0 {
			if dt := now.Sub(lastAt).Seconds(); dt > 0 {
				inst := float64(max(0, done-last)) / dt
				j.rate = 0.3*inst + 0.7*j.rate
			}
		}
		last, lastAt = done, now
		j.bytes = done
		j.stats = h.Stats()
		total := j.total
		var changed []File
		if complete || n%m.opts.SaveEvery == 0 {
			for _, f := range j.files {
				if f.Selected && saved[f.Index] != f.BytesDone {
					changed = append(changed, f)
				}
			}
		}
		j.mu.Unlock()

		for _, f := range changed {
			if err := m.opts.Store.SetDownloadFileProgress(ctx, j.id, f.Index, f.BytesDone); err == nil {
				saved[f.Index] = f.BytesDone
			}
		}
		if len(changed) > 0 {
			_ = m.opts.Store.SetDownloadProgress(ctx, j.id, done, total)
		}
		if complete {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-tick.C:
		}
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
