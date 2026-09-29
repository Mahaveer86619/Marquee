// Package tui implements Marquee's terminal interface. It talks to the core
// service only through its HTTP API.
package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"marquee/internal/catalog"
	"marquee/internal/download"
	"marquee/internal/release"
)

// Backend is the subset of the core API the interface uses.
type Backend interface {
	Status(ctx context.Context) (Status, error)
	Search(ctx context.Context, query string) (SearchResponse, error)
	Title(ctx context.Context, ref string) (catalog.Title, error)
	Episodes(ctx context.Context, ref string, season int) ([]catalog.Episode, error)
	Poster(ctx context.Context, ref string) ([]byte, error)
	Releases(ctx context.Context, q ReleaseQuery) (ReleasesResponse, error)
	StartDownload(ctx context.Context, r DownloadRequest) (download.Download, error)
	Downloads(ctx context.Context) ([]download.View, error)
	DownloadAction(ctx context.Context, id, action string) (download.View, error)
}

// DownloadRequest queues a release for download.
type DownloadRequest struct {
	ReleaseID string        `json:"release_id"`
	Ref       string        `json:"ref"`
	Scope     release.Scope `json:"scope"`
	Season    int           `json:"season"`
	Episode   int           `json:"episode"`
	Audio     []string      `json:"audio"`
	Subtitles []string      `json:"subtitles"`
}

// ReleaseQuery selects what to find releases for.
type ReleaseQuery struct {
	Ref     string        // film or series reference
	Scope   release.Scope // movie, episode, season or series
	Season  int
	Episode int
	Refresh bool
	Source  string // one source (progressive results); empty searches all
}

// ReleasesResponse is a release search result from the core.
type ReleasesResponse struct {
	Target      release.Target         `json:"target"`
	Releases    []release.Release      `json:"releases"`
	Sources     []release.SourceStatus `json:"sources"`
	Cached      bool                   `json:"cached"`
	Preferences struct {
		AudioLanguages    []string `json:"audio_languages"`
		SubtitleLanguages []string `json:"subtitle_languages"`
	} `json:"preferences"`
}

// Status is the core's report of configured providers.
type Status struct {
	Providers      map[string]bool      `json:"providers"`
	Checks         map[string]string    `json:"checks"`
	ReleaseSources []release.SourceInfo `json:"release_sources"`
}

// SearchResponse is the result of a metadata search.
type SearchResponse struct {
	Source  string                 `json:"source"`
	Results []catalog.SearchResult `json:"results"`
	Notice  string                 `json:"notice,omitempty"` // set when the core fell back to another provider
}

// Client is an HTTP client for the core API.
type Client struct {
	base string
	http *http.Client
}

// NewClient returns a client for the core service at base, e.g. http://127.0.0.1:7700.
func NewClient(base string) *Client {
	return &Client{base: base, http: &http.Client{Timeout: 3 * time.Minute}} // each call also has its own context deadline
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPost, path, body, out)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		var e struct {
			Error string `json:"error"`
		}
		if json.NewDecoder(resp.Body).Decode(&e) == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return fmt.Errorf("core returned HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) Status(ctx context.Context) (Status, error) {
	var s Status
	return s, c.get(ctx, "/api/v1/status", &s)
}

func (c *Client) Search(ctx context.Context, query string) (SearchResponse, error) {
	var r SearchResponse
	return r, c.get(ctx, "/api/v1/search?q="+url.QueryEscape(query), &r)
}

func (c *Client) Title(ctx context.Context, ref string) (catalog.Title, error) {
	var t catalog.Title
	return t, c.get(ctx, "/api/v1/titles/"+url.PathEscape(ref), &t)
}

// Releases searches for downloadable releases.
func (c *Client) Releases(ctx context.Context, q ReleaseQuery) (ReleasesResponse, error) {
	v := url.Values{"ref": {q.Ref}, "scope": {string(q.Scope)}}
	if q.Season > 0 || q.Scope == release.ScopeSeason || q.Scope == release.ScopeEpisode {
		v.Set("season", fmt.Sprint(q.Season))
	}
	if q.Episode > 0 {
		v.Set("episode", fmt.Sprint(q.Episode))
	}
	if q.Refresh {
		v.Set("refresh", "1")
	}
	if q.Source != "" {
		v.Set("source", q.Source)
	}
	var r ReleasesResponse
	return r, c.get(ctx, "/api/v1/releases?"+v.Encode(), &r)
}

// StartDownload queues a release.
func (c *Client) StartDownload(ctx context.Context, r DownloadRequest) (download.Download, error) {
	var d download.Download
	return d, c.post(ctx, "/api/v1/downloads", r, &d)
}

// Downloads lists the newest downloads.
func (c *Client) Downloads(ctx context.Context) ([]download.View, error) {
	var r struct {
		Downloads []download.View `json:"downloads"`
	}
	return r.Downloads, c.get(ctx, "/api/v1/downloads", &r)
}

// DownloadAction pauses, resumes or cancels a download.
func (c *Client) DownloadAction(ctx context.Context, id, action string) (download.View, error) {
	var v download.View
	return v, c.post(ctx, "/api/v1/downloads/"+url.PathEscape(id)+"/"+url.PathEscape(action), nil, &v)
}

// Poster returns the raw poster image of a title.
func (c *Client) Poster(ctx context.Context, ref string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v1/titles/"+url.PathEscape(ref)+"/poster", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("poster: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

func (c *Client) Episodes(ctx context.Context, ref string, season int) ([]catalog.Episode, error) {
	var r struct {
		Episodes []catalog.Episode `json:"episodes"`
	}
	err := c.get(ctx, fmt.Sprintf("/api/v1/titles/%s/seasons/%d", url.PathEscape(ref), season), &r)
	return r.Episodes, err
}
