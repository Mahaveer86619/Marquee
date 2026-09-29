// Package release finds downloadable releases for a film, episode, season or
// whole series: it queries the configured sources, parses release names,
// filters releases that do not match the requested scope and ranks the rest
// for browser playback. Marquee ships no indexer configuration; the built-in
// source is the Internet Archive (public domain and openly licensed films).
package release

import (
	"context"
	"strings"

	"marquee/internal/catalog"
)

// Scope is what the user wants to download.
type Scope string

const (
	ScopeMovie   Scope = "movie"
	ScopeEpisode Scope = "episode"
	ScopeSeason  Scope = "season"
	ScopeSeries  Scope = "series"
)

// Target describes what to search for.
type Target struct {
	Ref     string       `json:"ref"` // film, episode, season or series reference
	Scope   Scope        `json:"scope"`
	Kind    catalog.Kind `json:"kind"`
	Title   string       `json:"title"`
	Year    int          `json:"year,omitempty"`
	IMDbID  string       `json:"imdb_id,omitempty"`
	TMDBID  int          `json:"tmdb_id,omitempty"`
	TVDBID  int          `json:"tvdb_id,omitempty"`
	Season  int          `json:"season,omitempty"`
	Episode int          `json:"episode,omitempty"`
}

// Key identifies the target for caching (ref plus scope).
func (t Target) Key() string {
	return t.Ref + "|" + string(t.Scope)
}

// Release is one downloadable candidate.
type Release struct {
	ID          string      `json:"id"`
	Source      string      `json:"source"`            // "internet_archive", "prowlarr", "magnet"
	Indexer     string      `json:"indexer,omitempty"` // for Prowlarr results
	Name        string      `json:"name"`
	InfoHash    string      `json:"info_hash,omitempty"`
	Magnet      string      `json:"magnet,omitempty"`
	TorrentURL  string      `json:"torrent_url,omitempty"`
	PageURL     string      `json:"page_url,omitempty"`
	License     string      `json:"license,omitempty"`
	SizeBytes   int64       `json:"size_bytes,omitempty"`
	Seeders     int         `json:"seeders"` // -1 when unknown (web-seeded sources)
	Leechers    int         `json:"leechers"`
	Published   string      `json:"published,omitempty"`
	Parsed      Parsed      `json:"parsed"`
	IDs         ExternalIDs `json:"ids,omitempty"`          // title IDs tagged by the indexer, when given
	Files       []File      `json:"files,omitempty"`        // known before download for some sources
	PrimaryFile string      `json:"primary_file,omitempty"` // the video file this release stands for, when a torrent holds several
	Score       int         `json:"score"`
	Reasons     []string    `json:"reasons,omitempty"`
}

// ExternalIDs are title IDs an indexer attached to a result (0 = unknown).
type ExternalIDs struct {
	TMDB   int `json:"tmdb,omitempty"`
	IMDb   int `json:"imdb,omitempty"` // numeric part of "tt0816692"
	TVDB   int `json:"tvdb,omitempty"`
	TVmaze int `json:"tvmaze,omitempty"`
}

// File is a file inside a release, when the source lists them.
type File struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
	Kind      string `json:"kind"`               // "video", "subtitle", "audio", "other"
	Language  string `json:"language,omitempty"` // ISO 639-1, from the file name
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	Format    string `json:"format,omitempty"`
}

// Parsed is what the release name (and file list) tells us.
type Parsed struct {
	Title      string   `json:"title,omitempty"`
	Year       int      `json:"year,omitempty"`
	Resolution string   `json:"resolution,omitempty"` // "2160p", "1080p", "720p", "480p"
	Quality    string   `json:"quality,omitempty"`    // "WEB-DL", "BluRay", "CAM", ...
	Codec      string   `json:"codec,omitempty"`      // "avc", "hevc", "av1", ...
	Audio      []string `json:"audio,omitempty"`      // audio codecs, e.g. "Dolby Digital Plus"
	Channels   []string `json:"channels,omitempty"`
	HDR        []string `json:"hdr,omitempty"`
	BitDepth   string   `json:"bit_depth,omitempty"`
	Languages  []string `json:"languages,omitempty"` // ISO 639-1 audio languages advertised
	DualAudio  bool     `json:"dual_audio,omitempty"`
	Multi      bool     `json:"multi,omitempty"`     // "MULTi": several languages, not listed
	Subtitles  []string `json:"subtitles,omitempty"` // ISO 639-1 subtitle languages advertised
	Seasons    []int    `json:"seasons,omitempty"`
	Episodes   []int    `json:"episodes,omitempty"`
	Complete   bool     `json:"complete,omitempty"`
	Group      string   `json:"group,omitempty"`
	Trash      bool     `json:"trash,omitempty"` // camera or screener copies
	Repack     bool     `json:"repack,omitempty"`
}

// Prefs are the user's preferences that affect ranking.
type Prefs struct {
	AudioLanguages    []string // ISO 639-2 (config.json), e.g. "eng", "hin"
	SubtitleLanguages []string
}

// Source finds releases for a target.
type Source interface {
	Name() string
	// Supports reports whether the source can serve the scope at all.
	Supports(t Target) bool
	Search(ctx context.Context, t Target) ([]Release, error)
}

// ---------------------------------------------------------------------------
// Languages

type language struct{ one, two, name string }

var languageTable = []language{
	{"en", "eng", "English"}, {"hi", "hin", "Hindi"}, {"es", "spa", "Spanish"}, {"fr", "fre", "French"},
	{"de", "ger", "German"}, {"it", "ita", "Italian"}, {"ja", "jpn", "Japanese"}, {"ko", "kor", "Korean"},
	{"zh", "chi", "Chinese"}, {"ta", "tam", "Tamil"}, {"te", "tel", "Telugu"}, {"ml", "mal", "Malayalam"},
	{"kn", "kan", "Kannada"}, {"bn", "ben", "Bengali"}, {"mr", "mar", "Marathi"}, {"pa", "pan", "Punjabi"},
	{"ru", "rus", "Russian"}, {"pt", "por", "Portuguese"}, {"ar", "ara", "Arabic"}, {"tr", "tur", "Turkish"},
	{"pl", "pol", "Polish"}, {"nl", "dut", "Dutch"}, {"sv", "swe", "Swedish"}, {"da", "dan", "Danish"},
	{"no", "nor", "Norwegian"}, {"fi", "fin", "Finnish"}, {"th", "tha", "Thai"}, {"id", "ind", "Indonesian"},
	{"he", "heb", "Hebrew"}, {"fa", "per", "Persian"}, {"uk", "ukr", "Ukrainian"}, {"cs", "cze", "Czech"},
	{"el", "gre", "Greek"}, {"hu", "hun", "Hungarian"}, {"ro", "rum", "Romanian"}, {"vi", "vie", "Vietnamese"},
}

// Alternative ISO 639-2 "terminology" codes.
var twoAliases = map[string]string{"fra": "fr", "deu": "de", "zho": "zh", "nld": "nl", "fas": "fa", "ces": "cs", "ell": "el", "ron": "ro"}

// ToISO1 converts an ISO 639-2 or 639-1 code (or an English name) to 639-1.
func ToISO1(code string) string {
	c := strings.ToLower(strings.TrimSpace(code))
	if a, ok := twoAliases[c]; ok {
		return a
	}
	for _, l := range languageTable {
		if c == l.one || c == l.two || c == strings.ToLower(l.name) {
			return l.one
		}
	}
	return c
}

// LanguageName returns the English name of an ISO 639-1 code.
func LanguageName(code string) string {
	for _, l := range languageTable {
		if l.one == code {
			return l.name
		}
	}
	return strings.ToUpper(code)
}
