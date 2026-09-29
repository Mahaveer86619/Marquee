package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"marquee/internal/download"
)

// Messages for the downloads screen.
type (
	downloadsMsg struct {
		seq  int
		list []download.View
		err  error
	}
	downloadTickMsg struct{ seq int }
	queuedMsg       struct {
		d   download.Download
		err error
	}
	actionMsg struct {
		action string
		view   download.View
		err    error
	}
)

// downloadsState holds the downloads screen.
type downloadsState struct {
	list    []download.View
	cursor  int
	back    screen
	seq     int    // invalidates polls from an earlier visit
	confirm string // id awaiting a second x to cancel
	loaded  bool
}

// defaultPoll is how often the downloads screen refreshes.
const defaultPoll = time.Second

// WithPollInterval sets how often the downloads screen refreshes (tests use a
// short interval).
func (m Model) WithPollInterval(d time.Duration) Model {
	m.poll = d
	return m
}

func (m Model) downloadsCmd(seq int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := timeout()
		defer cancel()
		list, err := m.backend.Downloads(ctx)
		return downloadsMsg{seq: seq, list: list, err: err}
	}
}

func (m Model) startDownloadCmd(r DownloadRequest) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := timeout()
		defer cancel()
		d, err := m.backend.StartDownload(ctx, r)
		return queuedMsg{d: d, err: err}
	}
}

func (m Model) actionCmd(id, action string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := timeout()
		defer cancel()
		v, err := m.backend.DownloadAction(ctx, id, action)
		return actionMsg{action: action, view: v, err: err}
	}
}

// queueSelection sends the chosen release and languages to the core.
func (m *Model) queueSelection(sel Selection) tea.Cmd {
	q := m.rel.query
	m.loading = "Queueing " + sel.Release.Name + "..."
	return m.startDownloadCmd(DownloadRequest{
		ReleaseID: sel.Release.ID, Ref: q.Ref, Scope: q.Scope, Season: q.Season, Episode: q.Episode,
		Audio: sel.Audio, Subtitles: sel.Subtitles,
	})
}

func (m Model) handleQueued(msg queuedMsg) (tea.Model, tea.Cmd) {
	m.loading = ""
	if msg.err != nil {
		m.err = "Could not queue the download: " + msg.err.Error()
		return m, nil
	}
	m.err = ""
	m.rel.message = "Queued " + msg.d.Name + ". Press ctrl+d to see downloads."
	return m, nil
}

// openDownloads shows the downloads screen and starts polling.
func (m Model) openDownloads() (tea.Model, tea.Cmd) {
	if m.screen != screenDownloads {
		m.dl.back = m.screen
	}
	m.screen, m.err = screenDownloads, ""
	m.dl.seq++
	m.dl.confirm = ""
	return m, m.downloadsCmd(m.dl.seq)
}

func (m Model) handleDownloads(msg downloadsMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.dl.seq || m.screen != screenDownloads {
		return m, nil
	}
	if msg.err != nil {
		m.err = "Could not load downloads: " + msg.err.Error()
	} else {
		if m.err != "" && strings.HasPrefix(m.err, "Could not load downloads") {
			m.err = ""
		}
		m.dl.list, m.dl.loaded = msg.list, true
		m.dl.cursor = min(m.dl.cursor, max(0, len(m.dl.list)-1))
	}
	seq := m.dl.seq
	return m, tea.Tick(m.pollInterval(), func(time.Time) tea.Msg { return downloadTickMsg{seq: seq} })
}

func (m Model) pollInterval() time.Duration {
	if m.poll > 0 {
		return m.poll
	}
	return defaultPoll
}

func (m Model) handleDownloadTick(msg downloadTickMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.dl.seq || m.screen != screenDownloads {
		return m, nil
	}
	return m, m.downloadsCmd(m.dl.seq)
}

func (m Model) handleAction(msg actionMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.err = "Could not " + msg.action + " the download: " + msg.err.Error()
		return m, nil
	}
	m.err = ""
	for i := range m.dl.list {
		if m.dl.list[i].ID == msg.view.ID {
			m.dl.list[i] = msg.view
		}
	}
	return m, nil
}

func (m Model) updateDownloads(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if k != "x" {
		m.dl.confirm = ""
	}
	var cur *download.View
	if m.dl.cursor < len(m.dl.list) {
		cur = &m.dl.list[m.dl.cursor]
	}
	switch k {
	case "up", "k":
		m.dl.cursor = max(0, m.dl.cursor-1)
	case "down", "j":
		m.dl.cursor = min(max(0, len(m.dl.list)-1), m.dl.cursor+1)
	case "p", "space":
		if cur == nil {
			return m, nil
		}
		switch cur.State {
		case download.Paused, download.Failed:
			return m, m.actionCmd(cur.ID, "resume")
		case download.Queued, download.Metadata, download.Downloading:
			return m, m.actionCmd(cur.ID, "pause")
		}
	case "x":
		if cur == nil || cur.State.Final() {
			return m, nil
		}
		if m.dl.confirm != cur.ID {
			m.dl.confirm = cur.ID
			return m, nil
		}
		m.dl.confirm = ""
		return m, m.actionCmd(cur.ID, "cancel")
	case "esc", "backspace", "ctrl+d":
		m.screen, m.err = m.dl.back, ""
		m.dl.seq++ // stop polling
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Rendering

var stateLabels = map[download.State]string{
	download.Queued: "Queued", download.Metadata: "Getting file list", download.Downloading: "Downloading",
	download.Paused: "Paused", download.Finalizing: "Moving to library", download.Completed: "Completed",
	download.Failed: "Failed", download.Cancelled: "Cancelled",
}

func (m Model) viewDownloads() string {
	h := m.bodyHeight()
	var b strings.Builder
	b.WriteString(styleTitle.Render("Downloads"))
	b.WriteString("\n\n")
	if !m.dl.loaded {
		b.WriteString(styleDim.Render("Loading..."))
		return b.String()
	}
	if len(m.dl.list) == 0 {
		b.WriteString(styleDim.Render("Nothing downloaded yet. Open a title, press d, choose a release and confirm the options."))
		return b.String()
	}

	listW := min(m.width, 100)
	rows := max(1, (h-2)/3-3) // three lines per download, leaving room for details
	start, end := window(m.dl.cursor, len(m.dl.list), min(rows, len(m.dl.list)))
	for i := start; i < end; i++ {
		b.WriteString(m.downloadRow(m.dl.list[i], i == m.dl.cursor, listW))
	}
	if cur := m.dl.cursor; cur < len(m.dl.list) {
		b.WriteString("\n")
		b.WriteString(m.downloadDetail(m.dl.list[cur], listW))
	}
	return b.String()
}

func (m Model) downloadRow(v download.View, selected bool, width int) string {
	name := truncate(v.Name, width-4)
	if selected {
		name = styleSelected.Render(padRight("> "+name, width))
	} else {
		name = "  " + name
	}
	state := stateLabels[v.State]
	switch v.State {
	case download.Completed:
		state = styleOK.Render(state)
	case download.Failed:
		state = styleErr.Render(state)
	case download.Paused, download.Cancelled:
		state = styleDim.Render(state)
	}
	barW := max(10, min(40, width-50))
	line := fmt.Sprintf("  %s %5.1f%%  %s", progressBar(v.Progress, barW), v.Progress*100, state)
	if v.BytesTotal > 0 {
		line += styleDim.Render(fmt.Sprintf("  %s of %s", humanSize(v.BytesDone), humanSize(v.BytesTotal)))
	}
	if v.State == download.Downloading {
		line += styleDim.Render(fmt.Sprintf("  %s/s  %s  %d peers", humanSize(v.RateBps), etaText(v.ETASeconds), v.Peers))
	}
	if v.State == download.Failed && v.Error != "" {
		line += "  " + styleErr.Render(truncate(v.Error, max(10, width-barW-30)))
	}
	if m.dl.confirm == v.ID {
		line += "  " + styleWarn.Render("press x again to cancel and delete partial data")
	}
	return name + "\n" + line + "\n\n"
}

func (m Model) downloadDetail(v download.View, width int) string {
	var b strings.Builder
	b.WriteString(styleHeading.Render("Files"))
	b.WriteString("\n")
	shown := 0
	for _, f := range v.Files {
		if !f.Selected {
			continue
		}
		if shown == 8 {
			b.WriteString(styleDim.Render("  ...\n"))
			break
		}
		shown++
		pct := 0.0
		if f.Size > 0 {
			pct = float64(f.BytesDone) / float64(f.Size) * 100
		}
		label := f.Path
		if f.LibraryPath != "" {
			label = "Library: " + f.LibraryPath
		}
		kind := f.Kind
		if f.Language != "" {
			kind += " " + f.Language
		}
		b.WriteString(fmt.Sprintf("  %5.1f%%  %-11s %s\n", pct, kind, truncate(label, width-24)))
	}
	if shown == 0 {
		b.WriteString(styleDim.Render("  The file list arrives once the torrent's metadata is downloaded.\n"))
	}
	if skipped := len(v.Files) - countSelected(v.Files); skipped > 0 {
		b.WriteString(styleDim.Render(fmt.Sprintf("  %d other files in the torrent are not downloaded.\n", skipped)))
	}
	return b.String()
}

func countSelected(files []download.File) int {
	n := 0
	for _, f := range files {
		if f.Selected {
			n++
		}
	}
	return n
}

func progressBar(p float64, width int) string {
	p = min(1, max(0, p))
	full := int(p * float64(width))
	return styleOK.Render(strings.Repeat("█", full)) + styleSep.Render(strings.Repeat("░", width-full))
}

func etaText(s int64) string {
	switch {
	case s <= 0:
		return "--"
	case s < 60:
		return fmt.Sprintf("%ds left", s)
	case s < 3600:
		return fmt.Sprintf("%dm %02ds left", s/60, s%60)
	default:
		return fmt.Sprintf("%dh %02dm left", s/3600, s%3600/60)
	}
}
