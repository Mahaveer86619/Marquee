package release

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"marquee/internal/httpx"
)

// Prowlarr searches the indexers the user configured in Prowlarr, through
// Prowlarr's search API (all indexers at once). Only torrent results are kept.
type Prowlarr struct {
	BaseURL string // e.g. http://prowlarr:9696 from inside the stack
	apiKey  string
	client  *http.Client
}

// prowlarrTimeout is how long a Prowlarr search may take: Prowlarr queries
// every configured indexer before answering, so one slow indexer delays it.
const prowlarrTimeout = 90 * time.Second

// NewProwlarr returns the Prowlarr source.
func NewProwlarr(baseURL, apiKey string) *Prowlarr {
	return &Prowlarr{BaseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, client: httpx.NewClientWithTimeout(prowlarrTimeout)}
}

func (s *Prowlarr) Name() string { return "prowlarr" }

// Timeout overrides the default per-source search limit.
func (s *Prowlarr) Timeout() time.Duration { return prowlarrTimeout }

func (s *Prowlarr) Supports(Target) bool { return true }

// Newznab/Torznab category numbers.
const (
	catMovies = "2000"
	catTV     = "5000"
)

// Query builds the text query for a target.
func Query(t Target) string {
	switch t.Scope {
	case ScopeMovie:
		if t.Year > 0 {
			return fmt.Sprintf("%s %d", t.Title, t.Year)
		}
		return t.Title
	case ScopeEpisode:
		return fmt.Sprintf("%s S%02dE%02d", t.Title, t.Season, t.Episode)
	case ScopeSeason:
		return fmt.Sprintf("%s S%02d", t.Title, t.Season)
	}
	return t.Title
}

func (s *Prowlarr) Search(ctx context.Context, t Target) ([]Release, error) {
	cat := catTV
	if t.Scope == ScopeMovie {
		cat = catMovies
	}
	q := url.Values{"query": {Query(t)}, "type": {"search"}, "categories": {cat}, "limit": {"100"}}
	var body []struct {
		Title       string `json:"title"`
		Size        int64  `json:"size"`
		Seeders     *int   `json:"seeders"`
		Leechers    *int   `json:"leechers"`
		InfoHash    string `json:"infoHash"`
		MagnetURL   string `json:"magnetUrl"`
		DownloadURL string `json:"downloadUrl"`
		InfoURL     string `json:"infoUrl"`
		Indexer     string `json:"indexer"`
		PublishDate string `json:"publishDate"`
		Protocol    string `json:"protocol"` // "torrent", "usenet" or "unknown"
		IMDbID      int    `json:"imdbId"`
		TMDBID      int    `json:"tmdbId"`
		TVDBID      int    `json:"tvdbId"`
		TVmazeID    int    `json:"tvMazeId"`
	}
	header := http.Header{"X-Api-Key": {s.apiKey}}
	if err := httpx.GetJSON(ctx, s.client, s.BaseURL+"/api/v1/search?"+q.Encode(), header, &body); err != nil {
		return nil, err
	}
	out := make([]Release, 0, len(body))
	for _, r := range body {
		if !strings.EqualFold(r.Protocol, "torrent") {
			continue
		}
		rel := Release{
			Source: s.Name(), Indexer: r.Indexer, Name: r.Title, InfoHash: strings.ToLower(r.InfoHash),
			Magnet: r.MagnetURL, TorrentURL: r.DownloadURL, PageURL: r.InfoURL, SizeBytes: r.Size,
			Published: r.PublishDate,
			IDs:       ExternalIDs{TMDB: r.TMDBID, IMDb: r.IMDbID, TVDB: r.TVDBID, TVmaze: r.TVmazeID},
		}
		if r.Seeders != nil {
			rel.Seeders = *r.Seeders
		}
		if r.Leechers != nil {
			rel.Leechers = *r.Leechers
		}
		out = append(out, rel)
	}
	return out, nil
}
