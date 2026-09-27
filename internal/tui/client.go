// Package tui implements Marquee's terminal interface. It talks to the core
// service only through its HTTP API.
package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"marquee/internal/catalog"
)

// Backend is the subset of the core API the interface uses.
type Backend interface {
	Status(ctx context.Context) (Status, error)
	Search(ctx context.Context, query string) (SearchResponse, error)
	Title(ctx context.Context, ref string) (catalog.Title, error)
	Episodes(ctx context.Context, ref string, season int) ([]catalog.Episode, error)
	Poster(ctx context.Context, ref string) ([]byte, error)
}

// Status is the core's report of configured providers.
type Status struct {
	Providers map[string]bool   `json:"providers"`
	Checks    map[string]string `json:"checks"`
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
	return &Client{base: base, http: &http.Client{Timeout: 20 * time.Second}}
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
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
