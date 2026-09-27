package tui

import (
	"context"
	"fmt"
	"image"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"marquee/internal/catalog"
)

type screen int

const (
	screenSearch screen = iota
	screenTitle
	screenSeason
)

const (
	debounceDelay = 350 * time.Millisecond
	previewDelay  = 150 * time.Millisecond
	minQueryLen   = 2
	tmdbNotice    = "This product uses the TMDB API but is not endorsed or certified by TMDB. TVmaze data is licensed CC BY-SA 4.0."
)

// Messages produced by commands.
type (
	statusMsg struct {
		status Status
		err    error
	}
	debounceMsg struct{ seq int }
	previewMsg  struct{ seq int }
	searchMsg   struct {
		seq  int
		resp SearchResponse
		err  error
	}
	// detailMsg carries full title details, for the preview or for opening a title.
	detailMsg struct {
		ref   string
		title catalog.Title
		err   error
		open  bool
	}
	posterMsg struct {
		ref string
		img image.Image
		err error
	}
	episodesMsg struct {
		season   int
		episodes []catalog.Episode
		err      error
	}
)

// Model is the root Bubble Tea model.
type Model struct {
	backend Backend
	screen  screen

	input       textinput.Model
	listFocused bool
	seq         int
	focusOnDone bool
	results     []catalog.SearchResult
	source      string
	notice      string // explains a provider fallback
	resCursor   int
	previewSeq  int

	details  map[string]catalog.Title // full details by ref
	loadingD map[string]bool
	posters  map[string]image.Image // nil value = no poster available
	loadingP map[string]bool
	rendered map[string]string // rendered posters by "ref|cols|rows"

	title        catalog.Title
	seasonCursor int
	scroll       int

	season   int
	episodes []catalog.Episode
	epCursor int

	loading   string
	err       string
	status    *Status
	statusErr error

	width, height int
}

// New returns the interface model backed by the core API.
func New(backend Backend) Model {
	in := textinput.New()
	in.Placeholder = "Type a film or series name"
	in.CharLimit = 120
	in.Focus()
	return Model{
		backend: backend, input: in, width: 100, height: 30,
		details: map[string]catalog.Title{}, loadingD: map[string]bool{},
		posters: map[string]image.Image{}, loadingP: map[string]bool{}, rendered: map[string]string{},
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.statusCmd())
}

// ---------------------------------------------------------------------------
// Commands

func timeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

func (m Model) statusCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := timeout()
		defer cancel()
		s, err := m.backend.Status(ctx)
		return statusMsg{status: s, err: err}
	}
}

func (m Model) searchCmd(seq int, query string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := timeout()
		defer cancel()
		resp, err := m.backend.Search(ctx, query)
		return searchMsg{seq: seq, resp: resp, err: err}
	}
}

func (m Model) detailCmd(ref string, open bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := timeout()
		defer cancel()
		t, err := m.backend.Title(ctx, ref)
		return detailMsg{ref: ref, title: t, err: err, open: open}
	}
}

func (m Model) posterCmd(ref string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := timeout()
		defer cancel()
		data, err := m.backend.Poster(ctx, ref)
		if err != nil {
			return posterMsg{ref: ref, err: err}
		}
		img, err := DecodeImage(data)
		return posterMsg{ref: ref, img: img, err: err}
	}
}

func (m Model) episodesCmd(ref string, season int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := timeout()
		defer cancel()
		eps, err := m.backend.Episodes(ctx, ref, season)
		return episodesMsg{season: season, episodes: eps, err: err}
	}
}

// ensureLoaded returns commands that fetch details and the poster of ref if
// they are not cached or in flight.
func (m *Model) ensureLoaded(ref string, wantPoster bool) tea.Cmd {
	var cmds []tea.Cmd
	if _, ok := m.details[ref]; !ok && !m.loadingD[ref] {
		m.loadingD[ref] = true
		cmds = append(cmds, m.detailCmd(ref, false))
	}
	if _, ok := m.posters[ref]; wantPoster && !ok && !m.loadingP[ref] {
		m.loadingP[ref] = true
		cmds = append(cmds, m.posterCmd(ref))
	}
	return tea.Batch(cmds...)
}

func (m *Model) schedulePreview() tea.Cmd {
	m.previewSeq++
	seq := m.previewSeq
	return tea.Tick(previewDelay, func(time.Time) tea.Msg { return previewMsg{seq: seq} })
}

// ---------------------------------------------------------------------------
// Update

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(max(20, m.leftWidth()-10))
		m.rendered = map[string]string{}
		return m, nil

	case statusMsg:
		if msg.err != nil {
			m.status, m.statusErr = nil, msg.err
		} else {
			m.status, m.statusErr = &msg.status, nil
		}
		return m, nil

	case debounceMsg:
		q := strings.TrimSpace(m.input.Value())
		if msg.seq != m.seq || len(q) < minQueryLen {
			return m, nil
		}
		m.loading, m.err = "Searching...", ""
		return m, m.searchCmd(m.seq, q)

	case searchMsg:
		if msg.seq != m.seq {
			return m, nil // a newer search is in flight
		}
		m.loading = ""
		if msg.err != nil {
			m.err = "Search failed: " + msg.err.Error()
			return m, nil
		}
		m.err, m.results, m.source, m.notice, m.resCursor = "", msg.resp.Results, msg.resp.Source, msg.resp.Notice, 0
		if m.focusOnDone && len(m.results) > 0 {
			m.focusList()
		}
		m.focusOnDone = false
		if len(m.results) > 0 {
			return m, m.schedulePreview()
		}
		return m, nil

	case previewMsg:
		if msg.seq != m.previewSeq || len(m.results) == 0 {
			return m, nil
		}
		return m, m.ensureLoaded(m.results[m.resCursor].Ref, true)

	case detailMsg:
		delete(m.loadingD, msg.ref)
		if msg.err != nil {
			if msg.open {
				m.loading, m.err = "", "Could not load title: "+msg.err.Error()
			}
			return m, nil
		}
		m.details[msg.ref] = msg.title
		if msg.open {
			m.loading = ""
			return m, m.openTitle(msg.title)
		}
		return m, nil

	case posterMsg:
		delete(m.loadingP, msg.ref)
		if msg.err != nil {
			m.posters[msg.ref] = nil // remembered as unavailable
		} else {
			m.posters[msg.ref] = msg.img
		}
		return m, nil

	case episodesMsg:
		m.loading = ""
		if msg.err != nil {
			m.err = "Could not load episodes: " + msg.err.Error()
			return m, nil
		}
		m.err, m.season, m.episodes, m.epCursor, m.screen = "", msg.season, msg.episodes, 0, screenSeason
		return m, nil

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		switch m.screen {
		case screenSearch:
			return m.updateSearch(msg)
		case screenTitle:
			return m.updateTitle(msg)
		case screenSeason:
			return m.updateSeason(msg)
		}
	}

	if m.screen == screenSearch && !m.listFocused {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) openTitle(t catalog.Title) tea.Cmd {
	m.err, m.title, m.screen, m.scroll = "", t, screenTitle, 0
	m.seasonCursor = 0
	for i, s := range t.Seasons { // start on season 1 rather than specials
		if s.Number >= 1 {
			m.seasonCursor = i
			break
		}
	}
	return m.ensureLoaded(t.Ref, true)
}

func (m *Model) focusList() {
	m.listFocused = true
	m.input.Blur()
}

func (m *Model) focusInput() tea.Cmd {
	m.listFocused = false
	return m.input.Focus()
}

func (m Model) updateSearch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if !m.listFocused {
		switch key {
		case "enter":
			q := strings.TrimSpace(m.input.Value())
			if q == "" {
				return m, nil
			}
			m.seq++
			m.focusOnDone = true
			m.loading, m.err = "Searching...", ""
			return m, m.searchCmd(m.seq, q)
		case "down", "tab":
			if len(m.results) > 0 {
				m.focusList()
			}
			return m, nil
		case "esc":
			m.input.SetValue("")
			m.seq++
			return m, nil
		}
		before := m.input.Value()
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if m.input.Value() != before {
			m.seq++
			seq := m.seq
			return m, tea.Batch(cmd, tea.Tick(debounceDelay, func(time.Time) tea.Msg { return debounceMsg{seq: seq} }))
		}
		return m, cmd
	}

	move := func(to int) (tea.Model, tea.Cmd) {
		to = min(max(0, to), len(m.results)-1)
		if to == m.resCursor {
			return m, nil
		}
		m.resCursor = to
		return m, m.schedulePreview()
	}
	switch key {
	case "up", "k":
		return move(m.resCursor - 1)
	case "down", "j":
		return move(m.resCursor + 1)
	case "pgup":
		return move(m.resCursor - m.listRows())
	case "pgdown":
		return move(m.resCursor + m.listRows())
	case "home", "g":
		return move(0)
	case "end", "G":
		return move(len(m.results) - 1)
	case "enter":
		if len(m.results) == 0 {
			return m, nil
		}
		ref := m.results[m.resCursor].Ref
		if t, ok := m.details[ref]; ok {
			return m, m.openTitle(t)
		}
		m.loading, m.err = "Loading details...", ""
		return m, m.detailCmd(ref, true)
	case "esc", "tab", "/":
		return m, m.focusInput()
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) updateTitle(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	isSeries := m.title.Kind == catalog.Series && len(m.title.Seasons) > 0
	lines, seasonsStart := m.titleLines()
	h := m.bodyHeight()
	maxScroll := max(0, len(lines)-h)

	switch msg.String() {
	case "up", "k":
		if isSeries {
			m.seasonCursor = max(0, m.seasonCursor-1)
			m.scroll = keepVisible(m.scroll, seasonsStart+m.seasonCursor, h, maxScroll)
		} else {
			m.scroll = max(0, m.scroll-1)
		}
	case "down", "j":
		if isSeries {
			m.seasonCursor = min(len(m.title.Seasons)-1, m.seasonCursor+1)
			m.scroll = keepVisible(m.scroll, seasonsStart+m.seasonCursor, h, maxScroll)
		} else {
			m.scroll = min(maxScroll, m.scroll+1)
		}
	case "pgup":
		m.scroll = max(0, m.scroll-(h-2))
	case "pgdown", "space":
		m.scroll = min(maxScroll, m.scroll+(h-2))
	case "home", "g":
		m.scroll = 0
	case "end", "G":
		m.scroll = maxScroll
	case "enter":
		if isSeries {
			n := m.title.Seasons[m.seasonCursor].Number
			m.loading, m.err = fmt.Sprintf("Loading season %d...", n), ""
			return m, m.episodesCmd(m.title.Ref, n)
		}
	case "esc", "backspace":
		m.screen, m.err = screenSearch, ""
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func keepVisible(scroll, line, height, maxScroll int) int {
	switch {
	case line < scroll:
		scroll = line
	case line >= scroll+height:
		scroll = line - height + 1
	}
	return min(max(0, scroll), maxScroll)
}

func (m Model) updateSeason(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.epCursor = max(0, m.epCursor-1)
	case "down", "j":
		m.epCursor = min(len(m.episodes)-1, m.epCursor+1)
	case "pgup":
		m.epCursor = max(0, m.epCursor-m.listRows())
	case "pgdown":
		m.epCursor = min(len(m.episodes)-1, m.epCursor+m.listRows())
	case "esc", "backspace":
		m.screen, m.err = screenTitle, ""
	case "q":
		return m, tea.Quit
	}
	return m, nil
}
