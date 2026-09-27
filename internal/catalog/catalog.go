// Package catalog defines the metadata types shared by providers, storage, the
// API and the terminal UI.
package catalog

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Kind distinguishes films from series.
type Kind string

const (
	Movie  Kind = "movie"
	Series Kind = "series"
)

// SearchResult is one entry in a metadata search.
type SearchResult struct {
	Ref         string  `json:"ref"` // universal reference, e.g. "tmdb:tv:1399"
	Kind        Kind    `json:"kind"`
	Name        string  `json:"name"`
	Year        int     `json:"year,omitempty"`
	ReleaseDate string  `json:"release_date,omitempty"` // first air date for series
	Rating      float64 `json:"rating,omitempty"`       // 0-10
	Language    string  `json:"language,omitempty"`     // ISO 639-1
	Overview    string  `json:"overview,omitempty"`
	PosterURL   string  `json:"poster_url,omitempty"`
	Source      string  `json:"source"` // provider name
}

// DetailsVersion is bumped when Details gains fields, so cached titles
// fetched before the change are refreshed.
const DetailsVersion = 2

// Details holds display information beyond the core title fields.
type Details struct {
	Version      int           `json:"version"`
	Tagline      string        `json:"tagline,omitempty"`
	ReleaseDate  string        `json:"release_date,omitempty"` // first air date for series
	EndDate      string        `json:"end_date,omitempty"`     // last air date of an ended series
	Rating       float64       `json:"rating,omitempty"`
	VoteCount    int           `json:"vote_count,omitempty"`
	Language     string        `json:"language,omitempty"`
	Countries    []string      `json:"countries,omitempty"`
	Networks     []string      `json:"networks,omitempty"`
	Creators     []string      `json:"creators,omitempty"` // directors for films, creators for series
	Cast         []string      `json:"cast,omitempty"`
	SeasonCount  int           `json:"season_count,omitempty"`
	EpisodeCount int           `json:"episode_count,omitempty"`
	LastEpisode  *EpisodeBrief `json:"last_episode,omitempty"`
	NextEpisode  *EpisodeBrief `json:"next_episode,omitempty"`
	Homepage     string        `json:"homepage,omitempty"`
}

// EpisodeBrief identifies an episode for "last aired" and "next" displays.
type EpisodeBrief struct {
	Season  int    `json:"season"`
	Number  int    `json:"number"`
	Name    string `json:"name,omitempty"`
	AirDate string `json:"air_date,omitempty"`
}

// Title is a film or series with its details.
type Title struct {
	Ref          string    `json:"ref"`
	Kind         Kind      `json:"kind"`
	Source       string    `json:"source"`
	Name         string    `json:"name"`
	OriginalName string    `json:"original_name,omitempty"`
	Year         int       `json:"year,omitempty"`
	Overview     string    `json:"overview,omitempty"`
	Status       string    `json:"status,omitempty"`
	RuntimeMin   int       `json:"runtime_min,omitempty"`
	Genres       []string  `json:"genres,omitempty"`
	PosterURL    string    `json:"poster_url,omitempty"`
	TMDBID       int       `json:"tmdb_id,omitempty"`
	TVmazeID     int       `json:"tvmaze_id,omitempty"`
	TVDBID       int       `json:"tvdb_id,omitempty"`
	IMDbID       string    `json:"imdb_id,omitempty"`
	Seasons      []Season  `json:"seasons,omitempty"`
	Details      Details   `json:"details"`
	FetchedAt    time.Time `json:"fetched_at"`
}

// Season summarizes one season of a series.
type Season struct {
	Number       int    `json:"number"`
	Name         string `json:"name,omitempty"`
	AirDate      string `json:"air_date,omitempty"`
	EpisodeCount int    `json:"episode_count,omitempty"`
}

// Episode is one episode of a series.
type Episode struct {
	Ref        string  `json:"ref"` // e.g. "tmdb:tv:1399:s01e01"
	Season     int     `json:"season"`
	Number     int     `json:"number"`
	Name       string  `json:"name,omitempty"`
	Overview   string  `json:"overview,omitempty"`
	AirDate    string  `json:"air_date,omitempty"`
	RuntimeMin int     `json:"runtime_min,omitempty"`
	Rating     float64 `json:"rating,omitempty"`
}

// Ref is a parsed universal reference: "<source>:<type>:<id>".
type Ref struct {
	Source string // "tmdb" or "tvmaze"
	Type   string // "movie", "tv" or "show"
	ID     int
}

// ParseRef parses a title reference such as "tmdb:movie:603" or "tvmaze:show:82".
func ParseRef(s string) (Ref, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return Ref{}, fmt.Errorf("invalid reference %q", s)
	}
	id, err := strconv.Atoi(parts[2])
	if err != nil || id <= 0 {
		return Ref{}, fmt.Errorf("invalid reference %q", s)
	}
	return Ref{Source: parts[0], Type: parts[1], ID: id}, nil
}

// String formats the reference.
func (r Ref) String() string {
	return fmt.Sprintf("%s:%s:%d", r.Source, r.Type, r.ID)
}

// EpisodeRef builds the reference of an episode of the given series.
func EpisodeRef(seriesRef string, season, number int) string {
	return fmt.Sprintf("%s:s%02de%02d", seriesRef, season, number)
}

// YearOf extracts the year from an ISO date such as "2008-01-20".
func YearOf(date string) int {
	if len(date) < 4 {
		return 0
	}
	y, err := strconv.Atoi(date[:4])
	if err != nil {
		return 0
	}
	return y
}
