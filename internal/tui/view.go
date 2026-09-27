package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"marquee/internal/catalog"
)

var (
	styleBrand    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E5A00D"))
	styleDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("#8A8A8A"))
	styleLabel    = lipgloss.NewStyle().Foreground(lipgloss.Color("#9E9EB8"))
	styleSelected = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#3A3A5A"))
	styleOK       = lipgloss.NewStyle().Foreground(lipgloss.Color("#5FD787"))
	styleWarn     = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAF5F"))
	styleErr      = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5F5F"))
	styleHeading  = lipgloss.NewStyle().Bold(true)
	styleTitle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF"))
	styleTagline  = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("#B8B8B8"))
	styleRating   = lipgloss.NewStyle().Foreground(lipgloss.Color("#E5A00D"))
	styleSep      = lipgloss.NewStyle().Foreground(lipgloss.Color("#444455"))
)

// labelWidth is the width of the label column in fact lists.
const labelWidth = 13

// ---------------------------------------------------------------------------
// Frame: header, body sized to the terminal, footer.

func (m Model) View() tea.View {
	body := fitLines(m.body(), m.bodyHeight())
	status := ""
	if m.loading != "" {
		status = styleDim.Render(m.loading)
	}
	if m.err != "" {
		status = styleErr.Render(truncate(m.err, m.width))
	}
	content := m.header() + "\n" + status + "\n" + body + "\n" + m.footer()

	v := tea.NewView(content)
	v.AltScreen = true
	v.WindowTitle = "Marquee"
	return v
}

func (m Model) body() string {
	switch m.screen {
	case screenTitle:
		return m.viewTitle()
	case screenSeason:
		return m.viewSeason()
	default:
		return m.viewSearch()
	}
}

func (m Model) noticeLines() int {
	return lipgloss.Height(styleDim.Width(m.width).Render(tmdbNotice))
}

// bodyHeight is the number of lines between the header block and the footer.
func (m Model) bodyHeight() int {
	return max(6, m.height-2-1-m.noticeLines()) // header + status line, keys line, notice
}

// leftWidth is the width of the list pane in split layouts.
func (m Model) leftWidth() int {
	return min(max(m.width*2/5, 36), 72, m.width)
}

// listRows is the number of list rows visible in the search results pane.
func (m Model) listRows() int {
	return max(3, m.bodyHeight()-3)
}

func (m Model) header() string {
	left := styleBrand.Render("Marquee") + styleDim.Render("  Search films and series")
	var right string
	switch {
	case m.statusErr != nil:
		right = styleErr.Render("core not reachable (run: marquee up)")
	case m.status == nil:
		right = styleDim.Render("connecting...")
	default:
		switch m.status.Checks["tmdb"] {
		case "valid":
			right = styleOK.Render("core connected, TMDB ready")
		case "invalid":
			right = styleErr.Render("core connected, TMDB key rejected")
		case "unreachable":
			right = styleWarn.Render("core connected, TMDB unreachable (using TVmaze)")
		default:
			right = styleWarn.Render("core connected, series only (no TMDB key)")
		}
	}
	gap := max(2, m.width-lipgloss.Width(left)-lipgloss.Width(right))
	return left + strings.Repeat(" ", gap) + right
}

func (m Model) footer() string {
	var keys string
	switch {
	case m.screen == screenSearch && !m.listFocused:
		keys = "type to search   enter search   down results   esc clear   ctrl+c quit"
	case m.screen == screenSearch:
		keys = "up/down move   pgup/pgdn page   enter open   / search   q quit"
	case m.screen == screenTitle && m.title.Kind == catalog.Series:
		keys = "up/down season   enter episodes   pgup/pgdn scroll   esc back   q quit"
	case m.screen == screenTitle:
		keys = "up/down or pgup/pgdn scroll   esc back   q quit"
	default:
		keys = "up/down episode   pgup/pgdn page   esc back   q quit"
	}
	return styleDim.Render(keys) + "\n" + styleDim.Width(m.width).Render(tmdbNotice)
}

// ---------------------------------------------------------------------------
// Search screen: input, results list on the left, live preview on the right.

func (m Model) viewSearch() string {
	h := m.bodyHeight()
	top := "Search  " + m.input.View()
	notice := ""
	if m.notice != "" {
		notice = styleWarn.Render(truncate(m.notice, m.width))
	}
	if len(m.results) == 0 {
		msg := "Results appear as you type."
		if m.loading == "" && m.err == "" && len(strings.TrimSpace(m.input.Value())) >= minQueryLen && m.seq > 0 {
			msg = "No results."
		}
		return top + "\n" + notice + "\n\n" + styleDim.Render(msg)
	}

	paneH := h - 2
	lw := m.leftWidth()
	rw := m.width - lw - 3
	left := fitLines(m.resultsList(lw, paneH), paneH)
	sep := fitLines(strings.TrimSuffix(strings.Repeat(styleSep.Render(" | ")+"\n", paneH), "\n"), paneH)
	right := ""
	if rw >= 20 {
		right = fitLines(m.preview(rw, paneH), paneH)
	}
	panes := lipgloss.JoinHorizontal(lipgloss.Top, padBlock(left, lw), sep, right)
	return top + "\n" + notice + "\n" + panes
}

func (m Model) resultsList(width, height int) string {
	var b strings.Builder
	b.WriteString(styleDim.Render(fmt.Sprintf("%d results from %s", len(m.results), m.source)) + "\n")
	rows := height - 1
	start, end := window(m.resCursor, len(m.results), rows)
	for i := start; i < end; i++ {
		r := m.results[i]
		rating := ""
		if r.Rating > 0 {
			rating = fmt.Sprintf("%.1f", r.Rating)
		}
		name := r.Name + yearSuffix(r.Year)
		nameW := max(8, width-8-len(rating)-2)
		line := fmt.Sprintf(" %-7s%s", kindLabel(r.Kind), padRight(truncate(name, nameW), nameW)) + " " + rating
		switch {
		case i == m.resCursor && m.listFocused:
			line = styleSelected.Render(padRight(line, width))
		case i == m.resCursor:
			line = styleHeading.Render(line)
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// preview shows the highlighted result: poster, key facts and the overview.
func (m Model) preview(width, height int) string {
	r := m.results[m.resCursor]
	t, full := m.details[r.Ref]
	if !full {
		t = catalog.Title{Ref: r.Ref, Kind: r.Kind, Name: r.Name, Year: r.Year, Overview: r.Overview,
			Details: catalog.Details{ReleaseDate: r.ReleaseDate, Rating: r.Rating, Language: r.Language}}
	}

	posterRows := min(height-4, 22)
	posterCols := PosterCols(posterRows)
	factsW := width - posterCols - 2
	if factsW < 26 || posterRows < 8 {
		posterRows, posterCols, factsW = 0, 0, width
	}

	facts := m.factLines(t, factsW, true)
	top := facts
	if posterRows > 0 {
		top = lipgloss.JoinHorizontal(lipgloss.Top, m.posterBlock(r.Ref, posterCols, posterRows), "  ", facts)
	}
	used := lipgloss.Height(top)
	overview := ""
	if rest := height - used - 1; rest > 0 && t.Overview != "" {
		overview = "\n\n" + fitLines(wrap(t.Overview, width), rest-1)
	}
	return top + overview
}

// ---------------------------------------------------------------------------
// Title screen: poster and facts, overview, seasons. Scrolls as a whole.

func (m Model) viewTitle() string {
	lines, _ := m.titleLines()
	h := m.bodyHeight()
	start := min(m.scroll, max(0, len(lines)-h))
	end := min(len(lines), start+h)
	out := lines[start:end]
	if end < len(lines) && len(out) > 0 {
		out[len(out)-1] = styleDim.Render(fmt.Sprintf("  ... %d more lines (pgdn)", len(lines)-end))
	}
	return strings.Join(out, "\n")
}

// titleLines renders the whole title page and returns its lines and the index
// of the first season row (for keeping the selected season visible).
func (m Model) titleLines() ([]string, int) {
	t := m.title
	h := m.bodyHeight()
	posterRows := min(max(h-2, 8), 30)
	posterCols := PosterCols(posterRows)
	factsW := m.width - posterCols - 3
	if factsW < 30 {
		posterRows, posterCols, factsW = 0, 0, m.width
	}

	facts := m.factLines(t, factsW, false)
	top := facts
	if posterRows > 0 {
		top = lipgloss.JoinHorizontal(lipgloss.Top, m.posterBlock(t.Ref, posterCols, posterRows), "   ", facts)
	}
	lines := strings.Split(top, "\n")

	if t.Overview != "" {
		lines = append(lines, "", styleHeading.Render("Overview"))
		lines = append(lines, strings.Split(wrap(t.Overview, m.width), "\n")...)
	}

	seasonsStart := -1
	if t.Kind == catalog.Series {
		lines = append(lines, "", styleHeading.Render("Seasons"))
		if len(t.Seasons) == 0 {
			lines = append(lines, styleDim.Render("No season information."))
		}
		seasonsStart = len(lines)
		for i, s := range t.Seasons {
			name := s.Name
			if name == "" {
				name = fmt.Sprintf("Season %d", s.Number)
			}
			row := fmt.Sprintf(" %-24s %s", truncate(name, 24), seasonInfo(s))
			if i == m.seasonCursor {
				row = styleSelected.Render(padRight(row, min(m.width, 60)))
			}
			lines = append(lines, row)
		}
	}
	return lines, seasonsStart
}

// factLines renders the key facts of a title. compact limits the list for the
// search preview.
func (m Model) factLines(t catalog.Title, width int, compact bool) string {
	d := t.Details
	var b strings.Builder
	b.WriteString(styleTitle.Render(truncate(t.Name, width)) + "\n")
	sub := kindLabel(t.Kind)
	if t.Year > 0 {
		sub += "  " + fmt.Sprint(t.Year)
	}
	if t.Status != "" && t.Status != "Released" {
		sub += "  " + t.Status
	}
	b.WriteString(styleDim.Render(sub) + "\n")
	if d.Tagline != "" && !compact {
		b.WriteString(styleTagline.Render(wrap(d.Tagline, width)) + "\n")
	}
	b.WriteString("\n")

	kv := func(label, value string) {
		if value == "" {
			return
		}
		b.WriteString(factLine(label, value, width) + "\n")
	}

	if t.Kind == catalog.Series {
		kv("First aired", formatDate(d.ReleaseDate))
		kv("Ended", formatDate(d.EndDate))
	} else {
		kv("Released", formatDate(d.ReleaseDate))
	}
	kv("Rating", ratingText(d.Rating, d.VoteCount))
	if t.Kind == catalog.Series {
		kv("Seasons", seriesSize(t))
		if t.RuntimeMin > 0 {
			kv("Episode", fmt.Sprintf("%d min", t.RuntimeMin))
		}
		if !compact {
			kv("Last aired", episodeBrief(d.LastEpisode))
		}
		kv("Next episode", episodeBrief(d.NextEpisode))
	} else if t.RuntimeMin > 0 {
		kv("Runtime", formatRuntime(t.RuntimeMin))
	}
	kv("Genres", strings.Join(t.Genres, ", "))
	if t.Kind == catalog.Series {
		kv("Created by", strings.Join(d.Creators, ", "))
		kv("Network", strings.Join(d.Networks, ", "))
	} else {
		kv("Directed by", strings.Join(d.Creators, ", "))
	}
	cast := d.Cast
	if compact && len(cast) > 3 {
		cast = cast[:3]
	}
	kv("Starring", strings.Join(cast, ", "))
	if !compact {
		kv("Country", strings.Join(d.Countries, ", "))
		kv("Language", languageName(d.Language))
		if t.OriginalName != "" && t.OriginalName != t.Name {
			kv("Original title", t.OriginalName)
		}
		kv("IMDb", t.IMDbID)
		kv("Website", d.Homepage)
		kv("Source", t.Source)
	} else {
		kv("Language", languageName(d.Language))
	}
	return strings.TrimRight(b.String(), "\n")
}

// factLine renders "Label        value", wrapping the value under itself.
func factLine(label, value string, width int) string {
	valueW := max(10, width-labelWidth)
	wrapped := strings.Split(wrap(value, valueW), "\n")
	var b strings.Builder
	b.WriteString(styleLabel.Render(padRight(label, labelWidth)) + wrapped[0])
	for _, l := range wrapped[1:] {
		b.WriteString("\n" + strings.Repeat(" ", labelWidth) + l)
	}
	return b.String()
}

// posterBlock returns the rendered poster, or a placeholder while it loads.
func (m Model) posterBlock(ref string, cols, rows int) string {
	img, ok := m.posters[ref]
	switch {
	case !ok:
		return placeholderPoster(cols, rows, "loading")
	case img == nil:
		return placeholderPoster(cols, rows, "no poster")
	}
	key := fmt.Sprintf("%s|%d|%d", ref, cols, rows)
	if s, ok := m.rendered[key]; ok {
		return s
	}
	s := RenderImage(img, cols, rows)
	m.rendered[key] = s // map is shared across model copies, so this caches
	return s
}

// ---------------------------------------------------------------------------
// Season screen: episode list on the left, the selected episode on the right.

func (m Model) viewSeason() string {
	h := m.bodyHeight()
	heading := styleTitle.Render(fmt.Sprintf("%s  Season %d", m.title.Name, m.season))
	if len(m.episodes) == 0 {
		return heading + "\n\n" + styleDim.Render("No episodes listed for this season.")
	}
	paneH := h - 2
	lw := min(max(m.width/2, 40), 80, m.width)
	rw := m.width - lw - 3

	var list strings.Builder
	start, end := window(m.epCursor, len(m.episodes), paneH)
	for i := start; i < end; i++ {
		e := m.episodes[i]
		date := e.AirDate
		nameW := max(8, lw-9-len(date)-2)
		line := fmt.Sprintf(" S%02dE%02d  %s %s", e.Season, e.Number, padRight(truncate(e.Name, nameW), nameW), date)
		if i == m.epCursor {
			line = styleSelected.Render(padRight(line, lw))
		}
		list.WriteString(line + "\n")
	}

	e := m.episodes[m.epCursor]
	var detail strings.Builder
	detail.WriteString(styleTitle.Render(truncate(fmt.Sprintf("S%02dE%02d  %s", e.Season, e.Number, e.Name), rw)) + "\n\n")
	detail.WriteString(factLine("Aired", formatDate(e.AirDate), rw) + "\n")
	if e.RuntimeMin > 0 {
		detail.WriteString(factLine("Runtime", formatRuntime(e.RuntimeMin), rw) + "\n")
	}
	if e.Rating > 0 {
		detail.WriteString(factLine("Rating", ratingText(e.Rating, 0), rw) + "\n")
	}
	if e.Overview != "" {
		detail.WriteString("\n" + wrap(e.Overview, rw))
	}

	left := fitLines(strings.TrimSuffix(list.String(), "\n"), paneH)
	sep := fitLines(strings.TrimSuffix(strings.Repeat(styleSep.Render(" | ")+"\n", paneH), "\n"), paneH)
	right := ""
	if rw >= 20 {
		right = fitLines(detail.String(), paneH)
	}
	return heading + "\n\n" + lipgloss.JoinHorizontal(lipgloss.Top, padBlock(left, lw), sep, right)
}

// ---------------------------------------------------------------------------
// Formatting helpers

var months = [...]string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}

// formatDate turns "2014-10-22" into "22 Oct 2014".
func formatDate(s string) string {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return s
	}
	var y, mo, d int
	if _, err := fmt.Sscanf(s, "%d-%d-%d", &y, &mo, &d); err != nil || mo < 1 || mo > 12 {
		return s
	}
	return fmt.Sprintf("%d %s %d", d, months[mo-1], y)
}

func formatRuntime(min int) string {
	if min < 60 {
		return fmt.Sprintf("%d min", min)
	}
	return fmt.Sprintf("%dh %02dm", min/60, min%60)
}

func ratingText(r float64, votes int) string {
	if r <= 0 {
		return ""
	}
	s := styleRating.Render(fmt.Sprintf("%.1f", r)) + " / 10"
	if votes > 0 {
		s += styleDim.Render(fmt.Sprintf("  (%s votes)", thousands(votes)))
	}
	return s
}

func thousands(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func seriesSize(t catalog.Title) string {
	seasons, episodes := t.Details.SeasonCount, t.Details.EpisodeCount
	if seasons == 0 {
		for _, s := range t.Seasons {
			if s.Number >= 1 {
				seasons++
			}
		}
	}
	switch {
	case seasons == 0:
		return ""
	case episodes > 0:
		return fmt.Sprintf("%d seasons, %d episodes", seasons, episodes)
	default:
		return fmt.Sprintf("%d seasons", seasons)
	}
}

func episodeBrief(e *catalog.EpisodeBrief) string {
	if e == nil {
		return ""
	}
	s := fmt.Sprintf("S%02dE%02d", e.Season, e.Number)
	if e.Name != "" {
		s += " " + e.Name
	}
	if e.AirDate != "" {
		s += "  (" + formatDate(e.AirDate) + ")"
	}
	return s
}

var languages = map[string]string{
	"en": "English", "fr": "French", "es": "Spanish", "de": "German", "it": "Italian", "ja": "Japanese",
	"ko": "Korean", "zh": "Chinese", "hi": "Hindi", "ta": "Tamil", "te": "Telugu", "ml": "Malayalam",
	"kn": "Kannada", "bn": "Bengali", "mr": "Marathi", "ru": "Russian", "pt": "Portuguese", "sv": "Swedish",
	"da": "Danish", "no": "Norwegian", "nl": "Dutch", "tr": "Turkish", "pl": "Polish", "ar": "Arabic",
	"fa": "Persian", "th": "Thai", "id": "Indonesian", "he": "Hebrew", "fi": "Finnish", "cs": "Czech",
}

// languageName maps ISO 639-1 codes to names; other values pass through.
func languageName(code string) string {
	if n, ok := languages[code]; ok {
		return n
	}
	if len(code) == 2 {
		return strings.ToUpper(code)
	}
	return code
}

func kindLabel(k catalog.Kind) string {
	if k == catalog.Movie {
		return "Film"
	}
	return "Series"
}

func yearSuffix(y int) string {
	if y == 0 {
		return ""
	}
	return fmt.Sprintf(" (%d)", y)
}

func seasonInfo(s catalog.Season) string {
	var parts []string
	if s.EpisodeCount > 0 {
		parts = append(parts, fmt.Sprintf("%d episodes", s.EpisodeCount))
	}
	if y := catalog.YearOf(s.AirDate); y > 0 {
		parts = append(parts, fmt.Sprint(y))
	}
	return strings.Join(parts, ", ")
}

// window returns the slice bounds that keep the cursor visible.
func window(cursor, n, height int) (int, int) {
	if n <= height {
		return 0, n
	}
	start := min(max(0, cursor-height/2), n-height)
	return start, start + height
}

// wrap wraps plain text to width.
func wrap(s string, width int) string {
	return lipgloss.NewStyle().Width(max(10, width)).Render(s)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if n <= 3 || len(r) <= n {
		return s
	}
	return string(r[:n-3]) + "..."
}

func padRight(s string, w int) string {
	if gap := w - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

// padBlock pads every line of a block to width w.
func padBlock(s string, w int) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = padRight(l, w)
	}
	return strings.Join(lines, "\n")
}

// fitLines pads or cuts s to exactly n lines.
func fitLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	for len(lines) < n {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
