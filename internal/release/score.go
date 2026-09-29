package release

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Matches reports whether a parsed release is for the target and scope.
// Releases for other titles, years or episodes are dropped, not ranked.
func Matches(p Parsed, t Target) bool {
	return titleMatches(p.Title, t.Title) && scopeMatches(p, t)
}

// MatchesRelease is Matches, but an indexer-supplied ID that equals the
// target's (TMDB, IMDb or TVDB) replaces the title comparison, which catches
// releases named with translated or alternative titles. The scope must still match.
func MatchesRelease(r Release, t Target) bool {
	if idMatches(r.IDs, t) {
		return scopeMatches(r.Parsed, t)
	}
	return Matches(r.Parsed, t)
}

func idMatches(ids ExternalIDs, t Target) bool {
	switch {
	case ids.TMDB > 0 && t.TMDBID > 0:
		return ids.TMDB == t.TMDBID
	case ids.IMDb > 0 && t.IMDbID != "":
		return fmt.Sprintf("tt%07d", ids.IMDb) == t.IMDbID || fmt.Sprintf("tt%d", ids.IMDb) == t.IMDbID
	case ids.TVDB > 0 && t.TVDBID > 0:
		return ids.TVDB == t.TVDBID
	}
	return false
}

func scopeMatches(p Parsed, t Target) bool {
	switch t.Scope {
	case ScopeMovie:
		// Films: a series-looking release is not a film, and the year must be close.
		if len(p.Seasons) > 0 || len(p.Episodes) > 0 {
			return false
		}
		return p.Year == 0 || t.Year == 0 || abs(p.Year-t.Year) <= 1
	case ScopeEpisode:
		return slices.Contains(p.Seasons, t.Season) && slices.Contains(p.Episodes, t.Episode)
	case ScopeSeason:
		// A whole-season pack: the season is covered and no single episodes are listed.
		return slices.Contains(p.Seasons, t.Season) && len(p.Episodes) == 0
	case ScopeSeries:
		// A multi-season or complete-series pack.
		return len(p.Episodes) == 0 && (len(p.Seasons) > 1 || (p.Complete && len(p.Seasons) != 1))
	}
	return false
}

var nonWord = regexp.MustCompile(`[^a-z0-9]+`)

// normalizeTitle lowercases, drops accents and punctuation, and removes
// leading articles, so "The Martian" and "martian" compare equal.
func normalizeTitle(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r == '&':
			b.WriteString(" and ")
		case r < unicode.MaxASCII:
			b.WriteRune(r)
		default:
			b.WriteRune(foldAccent(r))
		}
	}
	n := strings.TrimSpace(nonWord.ReplaceAllString(b.String(), " "))
	for _, article := range []string{"the ", "a ", "an "} {
		n = strings.TrimPrefix(n, article)
	}
	return n
}

func foldAccent(r rune) rune {
	switch r {
	case 'à', 'á', 'â', 'ã', 'ä', 'å':
		return 'a'
	case 'è', 'é', 'ê', 'ë':
		return 'e'
	case 'ì', 'í', 'î', 'ï':
		return 'i'
	case 'ò', 'ó', 'ô', 'õ', 'ö':
		return 'o'
	case 'ù', 'ú', 'û', 'ü':
		return 'u'
	case 'ñ':
		return 'n'
	case 'ç':
		return 'c'
	}
	return ' '
}

func titleMatches(parsed, wanted string) bool {
	p, w := normalizeTitle(parsed), normalizeTitle(wanted)
	if p == "" || w == "" {
		return false
	}
	if p == w {
		return true
	}
	// Tolerate subtitles and suffixes: "Dune Part One" for "Dune", or a
	// release that drops a subtitle after a colon.
	if main, _, found := strings.Cut(wanted, ":"); found && normalizeTitle(main) == p {
		return true
	}
	return false
}

// Score ranks a release for browser playback and the user's preferences, and
// records the reasons shown in the interface.
func Score(r *Release, t Target, prefs Prefs) {
	p := r.Parsed
	score := 0
	var reasons []string
	add := func(points int, why string) {
		score += points
		sign := "+"
		if points < 0 {
			sign = ""
		}
		reasons = append(reasons, fmt.Sprintf("%s%d %s", sign, points, why))
	}

	if p.Trash {
		add(-200, "camera or screener copy")
	}

	switch p.Resolution {
	case "1080p":
		add(40, "1080p")
	case "2160p", "4k":
		add(30, "2160p (large files)")
	case "720p":
		add(25, "720p")
	case "576p", "480p", "360p":
		add(5, "standard definition")
	}

	switch p.Codec {
	case "avc", "h264", "x264":
		add(30, "H.264 plays in every browser")
	case "hevc", "h265", "x265":
		add(12, "HEVC needs hardware decoding in the browser")
	case "av1":
		add(8, "AV1 decodes in software on this machine")
	}

	if len(p.Audio) > 0 {
		a := strings.ToLower(strings.Join(p.Audio, " "))
		switch {
		case strings.Contains(a, "aac"):
			add(10, "AAC audio plays directly")
		case strings.Contains(a, "dts") || strings.Contains(a, "truehd"):
			add(2, "DTS/TrueHD audio is converted for the browser")
		default:
			add(6, "audio is converted cheaply if needed")
		}
	}

	switch {
	case r.Seeders < 0:
		add(25, "web seeded (downloads without peers)")
	case r.Seeders == 0:
		add(-40, "no seeders")
	default:
		add(min(40, int(math.Round(12*math.Log10(float64(r.Seeders)+1)))), fmt.Sprintf("%d seeders", r.Seeders))
	}

	offered := r.AudioLanguages()
	for i, lang := range prefs.AudioLanguages {
		if slices.Contains(offered, ToISO1(lang)) {
			add(max(5, 15-5*i), LanguageName(ToISO1(lang))+" audio")
		}
	}
	if len(r.SubtitleLanguages()) > 0 {
		add(5, "subtitles included")
	}

	if gb := float64(r.SizeBytes) / (1 << 30); r.SizeBytes > 0 {
		switch {
		case t.Scope == ScopeMovie && gb < 0.3:
			add(-20, "very small for a film")
		case t.Scope == ScopeMovie && gb > 40:
			add(-10, "very large (remux)")
		case t.Scope == ScopeEpisode && gb > 10:
			add(-10, "very large for an episode")
		}
	}
	if p.Repack {
		add(3, "repack or proper")
	}
	r.Score, r.Reasons = score, reasons
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
