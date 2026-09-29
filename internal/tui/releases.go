package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"marquee/internal/catalog"
	"marquee/internal/release"
)

// releasesMsg carries a release search result.
type releasesMsg struct {
	query ReleaseQuery
	resp  ReleasesResponse
	err   error
}

// choice is one checkbox in the download options panel.
type choice struct {
	code    string // ISO 639-1, or "*" for "all / not listed"
	label   string
	checked bool
}

// Selection is what the user chose to download.
type Selection struct {
	Release   release.Release
	Audio     []string // ISO 639-1 codes; "*" keeps every track
	Subtitles []string
}

// releaseState holds the releases and options screens.
type releaseState struct {
	query   ReleaseQuery
	label   string // e.g. "Loki  S01E02"
	back    screen // screen to return to
	resp    ReleasesResponse
	cursor  int // index into visibleReleases()
	loaded  bool
	showAll bool // include hidden (junk) releases
	audio   []choice
	subs    []choice
	optCur  int
	chosen  *Selection
	message string
}

// prefetchDelay is how long a highlight must rest before a background search.
const prefetchDelay = 800 * time.Millisecond

// prefetchMsg fires after the highlight rests on a season or episode.
type prefetchMsg struct {
	seq   int
	query ReleaseQuery
}

// queryKey identifies a release search regardless of source and refresh.
func queryKey(q ReleaseQuery) string {
	return fmt.Sprintf("%s|%s|%d|%d", q.Ref, q.Scope, q.Season, q.Episode)
}

// relEntry collects one search's answers source by source, so fast sources
// are shown while slow ones (e.g. Prowlarr) are still searching.
type relEntry struct {
	bySource     map[string]ReleasesResponse
	failed       map[string]string // source -> error text
	pending      map[string]bool
	unconfigured []release.SourceInfo
}

func newRelEntry() *relEntry {
	return &relEntry{bySource: map[string]ReleasesResponse{}, failed: map[string]string{}, pending: map[string]bool{}}
}

func (e *relEntry) done() bool     { return len(e.pending) == 0 }
func (e *relEntry) answered() bool { return len(e.bySource) > 0 || len(e.failed) > 0 }

// merged combines the per-source answers into one ranked response.
func (e *relEntry) merged() ReleasesResponse {
	var out ReleasesResponse
	names := make([]string, 0, len(e.bySource))
	for n := range e.bySource {
		names = append(names, n)
	}
	slices.Sort(names)

	seenRelease := map[string]int{}
	seenStatus := map[string]bool{}
	cached := len(names) > 0
	for _, name := range names {
		resp := e.bySource[name]
		out.Target, out.Preferences = resp.Target, resp.Preferences
		cached = cached && resp.Cached
		for _, s := range resp.Sources {
			if !seenStatus[s.Name] {
				seenStatus[s.Name] = true
				out.Sources = append(out.Sources, s)
			}
		}
		for _, r := range resp.Releases {
			if i, ok := seenRelease[r.ID]; ok { // same torrent from two sources
				if r.Score > out.Releases[i].Score {
					out.Releases[i] = r
				}
				continue
			}
			seenRelease[r.ID] = len(out.Releases)
			out.Releases = append(out.Releases, r)
		}
	}
	for name, msg := range e.failed {
		if !seenStatus[name] {
			seenStatus[name] = true
			out.Sources = append(out.Sources, release.SourceStatus{Name: name, Status: "error", Detail: msg})
		}
	}
	for name := range e.pending {
		if !seenStatus[name] {
			seenStatus[name] = true
			out.Sources = append(out.Sources, release.SourceStatus{Name: name, Status: "searching"})
		}
	}
	for _, u := range e.unconfigured {
		if !seenStatus[u.Name] {
			out.Sources = append(out.Sources, release.SourceStatus{Name: u.Name, Status: "not_configured", Detail: u.Detail})
		}
	}
	slices.SortStableFunc(out.Releases, func(a, b release.Release) int {
		if a.Score != b.Score {
			return b.Score - a.Score
		}
		return b.Seeders - a.Seeders
	})
	out.Cached = cached && e.done()
	return out
}

// releaseSources returns the sources to query one by one. Without the list
// from the core (older core, or status not loaded yet) one combined request
// is made.
func (m Model) releaseSources() ([]string, []release.SourceInfo) {
	var names []string
	var unconfigured []release.SourceInfo
	if m.status != nil {
		for _, s := range m.status.ReleaseSources {
			if s.Configured {
				names = append(names, s.Name)
			} else {
				unconfigured = append(unconfigured, s)
			}
		}
	}
	if len(names) == 0 {
		return []string{""}, nil
	}
	return names, unconfigured
}

func (m Model) releasesCmd(q ReleaseQuery) tea.Cmd {
	return func() tea.Msg {
		// Release searches wait on Prowlarr's indexers, so they get a longer limit.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		resp, err := m.backend.Releases(ctx, q)
		return releasesMsg{query: q, resp: resp, err: err}
	}
}

// startSearch queries every source separately, unless the search is already
// running or complete (a refresh always starts over).
func (m *Model) startSearch(q ReleaseQuery) tea.Cmd {
	k := queryKey(q)
	e := m.relCache[k]
	switch {
	case e == nil || q.Refresh:
		e = newRelEntry()
		m.relCache[k] = e
	case !e.done():
		return nil // already searching
	case e.answered():
		return nil // already complete
	}
	names, unconfigured := m.releaseSources()
	e.unconfigured = unconfigured
	cmds := make([]tea.Cmd, 0, len(names))
	for _, name := range names {
		sq := q
		sq.Source = name
		e.pending[name] = true
		cmds = append(cmds, m.releasesCmd(sq))
	}
	return tea.Batch(cmds...)
}

// schedulePrefetch searches in the background once the highlight has rested,
// so results are ready (or already underway) when the user presses d.
func (m *Model) schedulePrefetch(q ReleaseQuery) tea.Cmd {
	if m.relCache[queryKey(q)] != nil {
		return nil
	}
	m.prefetchSeq++
	seq := m.prefetchSeq
	return tea.Tick(prefetchDelay, func(time.Time) tea.Msg { return prefetchMsg{seq: seq, query: q} })
}

func (m Model) handlePrefetch(msg prefetchMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.prefetchSeq {
		return m, nil // the highlight moved on
	}
	if m.relCache[queryKey(msg.query)] != nil {
		return m, nil
	}
	return m, m.startSearch(msg.query)
}

// currentReleaseQuery is the search that d would run on the current screen.
func (m Model) currentReleaseQuery() (ReleaseQuery, bool) {
	switch {
	case m.screen == screenTitle && m.title.Kind == catalog.Movie:
		return ReleaseQuery{Ref: m.title.Ref, Scope: release.ScopeMovie}, true
	case m.screen == screenTitle && m.title.Kind == catalog.Series && len(m.title.Seasons) > 0:
		return ReleaseQuery{Ref: m.title.Ref, Scope: release.ScopeSeason, Season: m.title.Seasons[m.seasonCursor].Number}, true
	case m.screen == screenSeason && len(m.episodes) > 0:
		e := m.episodes[m.epCursor]
		return ReleaseQuery{Ref: m.title.Ref, Scope: release.ScopeEpisode, Season: e.Season, Episode: e.Number}, true
	}
	return ReleaseQuery{}, false
}

// prefetchCurrent schedules a background search for what d would open.
func (m *Model) prefetchCurrent() tea.Cmd {
	if q, ok := m.currentReleaseQuery(); ok {
		return m.schedulePrefetch(q)
	}
	return nil
}

// releaseHint describes background search progress for the status line.
func (m Model) releaseHint() string {
	q, ok := m.currentReleaseQuery()
	if !ok {
		return ""
	}
	e := m.relCache[queryKey(q)]
	if e == nil {
		return ""
	}
	n := 0
	for _, r := range e.merged().Releases {
		if recommended(r) {
			n++
		}
	}
	switch {
	case !e.done() && n > 0:
		return styleOK.Render(fmt.Sprintf("Releases: %d so far (press d), still searching %s...", n, pendingNames(e)))
	case !e.done():
		return styleDim.Render("Releases: searching in the background...")
	case n == 0:
		return styleDim.Render("Releases: none found for this selection")
	}
	return styleOK.Render(fmt.Sprintf("Releases: %d ready (press d)", n))
}

func pendingNames(e *relEntry) string {
	var names []string
	for n := range e.pending {
		names = append(names, sourceName(n))
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}

// openReleases shows releases, from the prefetch cache when available; a
// search still underway keeps filling in.
func (m Model) openReleases(q ReleaseQuery, label string) (tea.Model, tea.Cmd) {
	m.rel = releaseState{query: q, label: label, back: m.screen}
	m.screen = screenReleases
	m.err = ""
	if e := m.relCache[queryKey(q)]; e != nil {
		m.showEntry(e)
		return m, nil
	}
	m.loading = "Searching for releases..."
	return m, m.startSearch(q)
}

// showEntry displays the merged results, keeping the highlighted release
// selected as new sources arrive.
func (m *Model) showEntry(e *relEntry) {
	selected := ""
	if vs := m.visibleReleases(); m.rel.loaded && m.rel.cursor < len(vs) {
		selected = vs[m.rel.cursor].ID
	}
	m.rel.resp, m.rel.loaded = e.merged(), e.answered()
	m.rel.cursor = 0
	for i, r := range m.visibleReleases() {
		if r.ID == selected {
			m.rel.cursor = i
			break
		}
	}
	switch {
	case !e.done():
		m.loading = "Still searching " + pendingNames(e) + "..."
	default:
		m.loading = ""
	}
}

func (m Model) handleReleases(msg releasesMsg) (tea.Model, tea.Cmd) {
	k := queryKey(msg.query)
	e := m.relCache[k]
	if e == nil { // result for a search that was reset
		return m, nil
	}
	name := msg.query.Source
	if name == "" {
		name = "all"
	}
	delete(e.pending, msg.query.Source)
	if msg.err != nil {
		e.failed[name] = msg.err.Error()
	} else {
		e.bySource[name] = msg.resp
	}
	onScreen := (m.screen == screenReleases || m.screen == screenOptions) && queryKey(m.rel.query) == k
	if !onScreen {
		return m, nil // a background prefetch progressed
	}
	m.showEntry(e)
	if e.done() && len(e.bySource) == 0 && msg.err != nil {
		m.err = "Release search failed: " + msg.err.Error()
	}
	return m, nil
}

// recommended hides camera copies, dead torrents and negative scores.
func recommended(r release.Release) bool {
	return r.Score >= 0 && r.Seeders != 0 && !r.Parsed.Trash
}

// visibleReleases applies the junk filter unless the user chose to see all.
func (m Model) visibleReleases() []release.Release {
	if m.rel.showAll {
		return m.rel.resp.Releases
	}
	var out []release.Release
	for _, r := range m.rel.resp.Releases {
		if recommended(r) {
			out = append(out, r)
		}
	}
	return out
}

func (m Model) selectedRelease() release.Release {
	return m.visibleReleases()[m.rel.cursor]
}

func (m Model) updateReleases(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	n := len(m.visibleReleases())
	switch msg.String() {
	case "up", "k":
		m.rel.cursor = max(0, m.rel.cursor-1)
	case "down", "j":
		m.rel.cursor = min(max(0, n-1), m.rel.cursor+1)
	case "pgup":
		m.rel.cursor = max(0, m.rel.cursor-m.listRows())
	case "pgdown":
		m.rel.cursor = min(max(0, n-1), m.rel.cursor+m.listRows())
	case "f":
		m.rel.showAll, m.rel.cursor = !m.rel.showAll, 0
	case "r":
		q := m.rel.query
		q.Refresh = true
		m.rel.query, m.rel.loaded, m.rel.message = q, false, ""
		m.loading, m.err = "Refreshing releases...", ""
		return m, m.startSearch(q)
	case "enter":
		if n > 0 {
			m.prepareOptions()
			m.screen = screenOptions
		}
	case "esc", "backspace":
		m.screen, m.err = m.rel.back, ""
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

// prepareOptions builds the audio and subtitle checkboxes for the selected
// release, pre-ticking the user's preferred languages.
func (m *Model) prepareOptions() {
	r := m.selectedRelease()
	prefA := m.rel.resp.Preferences.AudioLanguages
	prefS := m.rel.resp.Preferences.SubtitleLanguages

	m.rel.audio = nil
	offered := r.AudioLanguages()
	for _, code := range offered {
		m.rel.audio = append(m.rel.audio, choice{code: code, label: release.LanguageName(code), checked: slices.Contains(prefA, code)})
	}
	if len(offered) == 0 {
		label := "Original audio (languages not listed by the release)"
		if r.Parsed.Multi {
			label = "All audio tracks (MULTi, languages not listed)"
		}
		m.rel.audio = []choice{{code: "*", label: label, checked: true}}
	} else if !slices.ContainsFunc(m.rel.audio, func(c choice) bool { return c.checked }) {
		for i := range m.rel.audio { // no preferred language offered: keep everything
			m.rel.audio[i].checked = true
		}
	}

	m.rel.subs = nil
	for _, code := range r.SubtitleLanguages() {
		m.rel.subs = append(m.rel.subs, choice{code: code, label: release.LanguageName(code), checked: slices.Contains(prefS, code)})
	}
	m.rel.optCur = 0
}

func (m Model) updateOptions(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	total := len(m.rel.audio) + len(m.rel.subs)
	toggle := func(i int) {
		if i < len(m.rel.audio) {
			m.rel.audio[i].checked = !m.rel.audio[i].checked
		} else if j := i - len(m.rel.audio); j < len(m.rel.subs) {
			m.rel.subs[j].checked = !m.rel.subs[j].checked
		}
	}
	switch msg.String() {
	case "up", "k":
		m.rel.optCur = max(0, m.rel.optCur-1)
	case "down", "j", "tab":
		m.rel.optCur = min(max(0, total-1), m.rel.optCur+1)
	case "space":
		toggle(m.rel.optCur)
	case "a":
		// Toggle every box in the current section.
		section := m.rel.audio
		if m.rel.optCur >= len(m.rel.audio) {
			section = m.rel.subs
		}
		all := !slices.ContainsFunc(section, func(c choice) bool { return !c.checked })
		for i := range section {
			section[i].checked = !all
		}
	case "enter":
		sel := Selection{Release: m.selectedRelease()}
		for _, c := range m.rel.audio {
			if c.checked {
				sel.Audio = append(sel.Audio, c.code)
			}
		}
		for _, c := range m.rel.subs {
			if c.checked {
				sel.Subtitles = append(sel.Subtitles, c.code)
			}
		}
		if len(sel.Audio) == 0 {
			m.err = "Choose at least one audio language."
			return m, nil
		}
		m.rel.chosen = &sel
		m.rel.message = ""
		m.screen, m.err = screenReleases, ""
		return m, m.queueSelection(sel)
	case "esc", "backspace":
		m.screen, m.err = screenReleases, ""
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

// Chosen returns the most recent download selection.
func (m Model) Chosen() *Selection { return m.rel.chosen }

// ---------------------------------------------------------------------------
// Rendering

var sourceNames = map[string]string{"internet_archive": "Internet Archive", "prowlarr": "Prowlarr", "magnet": "Magnet link"}

func sourceName(s string) string {
	if n, ok := sourceNames[s]; ok {
		return n
	}
	return s
}

func (m Model) viewReleases() string {
	h := m.bodyHeight()
	var b strings.Builder
	b.WriteString(styleTitle.Render("Releases  " + m.rel.label))
	b.WriteString(styleDim.Render("  (" + string(m.rel.query.Scope) + ")"))
	b.WriteString("\n")
	b.WriteString(m.sourcesLine())
	b.WriteString("\n")
	if m.rel.message != "" {
		b.WriteString(styleOK.Render(truncate(m.rel.message, m.width)))
		b.WriteString("\n")
	} else {
		b.WriteString("\n")
	}
	if !m.rel.loaded {
		return b.String()
	}
	all := m.rel.resp.Releases
	rs := m.visibleReleases()
	if len(all) == 0 {
		b.WriteString(styleDim.Render(wrap(m.noReleasesHint(), m.width)))
		return b.String()
	}
	if hidden := len(all) - len(rs); hidden > 0 {
		b.WriteString(styleDim.Render(fmt.Sprintf("%d of %d shown; %d hidden (camera copies, no seeders or low score). Press f to show all.", len(rs), len(all), hidden)))
		b.WriteString("\n")
	} else if m.rel.showAll {
		b.WriteString(styleDim.Render("Showing all results. Press f to hide low-quality ones."))
		b.WriteString("\n")
	}
	if len(rs) == 0 {
		return b.String()
	}

	detailH := min(10, max(5, h/3))
	listH := h - 3 - detailH - 1
	header := fmt.Sprintf(" %5s  %-6s %-6s %-18s %-10s %9s %6s  %-16s %s", "Score", "Res", "Codec", "Audio", "Subs", "Size", "Seeds", "Source", "Name")
	b.WriteString(styleLabel.Render(truncate(header, m.width)))
	b.WriteString("\n")
	start, end := window(m.rel.cursor, len(rs), max(1, listH-1))
	for i := start; i < end; i++ {
		row := releaseRow(rs[i], m.width)
		if i == m.rel.cursor {
			row = styleSelected.Render(padRight(row, m.width))
		} else if rs[i].Score < 0 {
			row = styleDim.Render(row)
		}
		b.WriteString(row)
		b.WriteString("\n")
	}
	used := lipgloss.Height(b.String())
	b.WriteString(strings.Repeat("\n", max(0, h-detailH-used)))
	b.WriteString(m.releaseDetail(rs[m.rel.cursor], detailH))
	return b.String()
}

func releaseRow(r release.Release, width int) string {
	audio := strings.Join(langNames(r.AudioLanguages()), ", ")
	if audio == "" && r.Parsed.Multi {
		audio = "multiple"
	}
	subs := strings.Join(r.SubtitleLanguages(), ",")
	seeds := fmt.Sprint(r.Seeders)
	switch {
	case r.Seeders == -1:
		seeds = "web"
	case r.Seeders < -1:
		seeds = "?"
	}
	src := sourceName(r.Source)
	if r.Indexer != "" {
		src = r.Indexer
	}
	row := fmt.Sprintf(" %5d  %-6s %-6s %-18s %-10s %9s %6s  %-16s ",
		r.Score, dash(r.Parsed.Resolution), dash(r.Parsed.Codec), truncate(dash(audio), 18), truncate(dash(subs), 10),
		humanSize(r.SizeBytes), seeds, truncate(src, 16))
	return row + truncate(r.Name, max(10, width-lipgloss.Width(row)))
}

func (m Model) releaseDetail(r release.Release, height int) string {
	var b strings.Builder
	b.WriteString(styleSep.Render(strings.Repeat("-", m.width)))
	b.WriteString("\n")
	b.WriteString(styleTitle.Render(truncate(r.Name, m.width)))
	b.WriteString("\n")
	kv := func(label, value string) {
		if value != "" {
			b.WriteString(factLine(label, value, m.width))
			b.WriteString("\n")
		}
	}
	kv("Audio", describeLanguages(r.AudioLanguages(), r.Parsed.Multi, r.Parsed.DualAudio))
	kv("Subtitles", strings.Join(langNames(r.SubtitleLanguages()), ", "))
	kv("Quality", strings.Join(nonEmpty(r.Parsed.Quality, strings.Join(r.Parsed.HDR, " "), r.Parsed.BitDepth, strings.Join(r.Parsed.Audio, " "), strings.Join(r.Parsed.Channels, " ")), "  "))
	kv("Why", strings.Join(r.Reasons, "  |  "))
	if r.License != "" {
		kv("Licence", r.License)
	}
	kv("Page", r.PageURL)
	return fitLines(strings.TrimRight(b.String(), "\n"), height)
}

func (m Model) sourcesLine() string {
	var parts []string
	for _, s := range m.rel.resp.Sources {
		name := sourceName(s.Name)
		switch s.Status {
		case "ok", "cached":
			parts = append(parts, styleOK.Render(fmt.Sprintf("%s: %d%s", name, s.Count, seconds(s.Ms))))
		case "error":
			parts = append(parts, styleErr.Render(name+": "+truncate(s.Detail, 40)+seconds(s.Ms)))
		case "searching":
			parts = append(parts, styleWarn.Render(name+": searching..."))
		default:
			parts = append(parts, styleDim.Render(name+": "+s.Detail))
		}
	}
	if m.rel.resp.Cached {
		parts = append(parts, styleDim.Render("cached (r to refresh)"))
	}
	return truncate(strings.Join(parts, styleDim.Render("   ")), m.width*3)
}

func (m Model) noReleasesHint() string {
	if m.rel.query.Scope == release.ScopeMovie {
		return "No releases found. The built-in Internet Archive source covers public-domain and openly licensed films. Enable Prowlarr (scripts\\full-up.ps1 -WithIndexers) to search indexers you configure."
	}
	return "No releases found. The built-in Internet Archive source covers films only. For series, enable Prowlarr (scripts\\full-up.ps1 -WithIndexers) and add indexers you are entitled to use."
}

func (m Model) viewOptions() string {
	r := m.selectedRelease()
	var b strings.Builder
	b.WriteString(styleTitle.Render("Download options"))
	b.WriteString("\n")
	b.WriteString(styleDim.Render(truncate(r.Name, m.width)))
	b.WriteString("\n\n")

	idx := 0
	box := func(c choice) string {
		mark := "[ ]"
		if c.checked {
			mark = "[x]"
		}
		line := fmt.Sprintf("  %s %s", mark, c.label)
		if idx == m.rel.optCur {
			line = styleSelected.Render(padRight(line, min(m.width, 70)))
		}
		idx++
		return line + "\n"
	}
	b.WriteString(styleHeading.Render("Audio languages to keep"))
	b.WriteString("\n")
	for _, c := range m.rel.audio {
		b.WriteString(box(c))
	}
	b.WriteString("\n")
	b.WriteString(styleHeading.Render("Subtitles to keep"))
	b.WriteString("\n")
	if len(m.rel.subs) == 0 {
		b.WriteString(styleDim.Render("  No subtitle languages listed by this release."))
		b.WriteString("\n")
	}
	for _, c := range m.rel.subs {
		b.WriteString(box(c))
	}
	b.WriteString("\n")
	b.WriteString(styleDim.Render(wrap("Languages come from the release name and file list. The exact audio and subtitle tracks inside the video are confirmed when the download starts; tracks you did not choose are removed without re-encoding.", min(m.width, 100))))
	return b.String()
}

// ---------------------------------------------------------------------------
// Helpers

func langNames(codes []string) []string {
	out := make([]string, 0, len(codes))
	for _, c := range codes {
		out = append(out, release.LanguageName(c))
	}
	return out
}

func describeLanguages(codes []string, multi, dual bool) string {
	s := strings.Join(langNames(codes), ", ")
	switch {
	case s == "" && multi:
		return "several languages (not listed)"
	case s == "":
		return "not listed (original audio)"
	case dual:
		return s + " (dual audio)"
	}
	return s
}

// seconds formats a duration in milliseconds as " (3.2 s)", or "" when unknown.
func seconds(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return fmt.Sprintf(" (%.1f s)", float64(ms)/1000)
}

func humanSize(n int64) string {
	switch {
	case n <= 0:
		return "-"
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	default:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	}
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func nonEmpty(vals ...string) []string {
	var out []string
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return out
}

// releaseLabel names the target of a query for headings.
func releaseLabel(t catalog.Title, q ReleaseQuery) string {
	switch q.Scope {
	case release.ScopeEpisode:
		return fmt.Sprintf("%s  S%02dE%02d", t.Name, q.Season, q.Episode)
	case release.ScopeSeason:
		return fmt.Sprintf("%s  Season %d", t.Name, q.Season)
	case release.ScopeSeries:
		return t.Name + "  (all seasons)"
	}
	return t.Name + yearSuffix(t.Year)
}
