// Package metadata searches and fetches film and series metadata from TMDB
// and TVmaze, and caches it in the core database.
package metadata

import (
	"context"
	"html"
	"net/http"
	"regexp"
	"strings"

	"marquee/internal/catalog"
	"marquee/internal/httpx"
)

// Provider is a metadata source.
type Provider interface {
	Name() string
	Search(ctx context.Context, query string) ([]catalog.SearchResult, error)
	Title(ctx context.Context, ref catalog.Ref) (catalog.Title, error)
	Episodes(ctx context.Context, ref catalog.Ref, season int) ([]catalog.Episode, error)
}

// Errors, shared with the HTTP layer so errors.Is works across packages.
var (
	ErrUnauthorized = httpx.ErrUnauthorized
	ErrNotFound     = httpx.ErrNotFound
	ErrRateLimited  = httpx.ErrRateLimited
)

func getJSON(ctx context.Context, client *http.Client, rawURL string, header http.Header, out any) error {
	return httpx.GetJSON(ctx, client, rawURL, header, out)
}

func newHTTPClient() *http.Client { return httpx.NewClient() }

var tagPattern = regexp.MustCompile(`<[^>]*>`)

// plainText strips HTML tags and entities (TVmaze summaries are HTML).
func plainText(s string) string {
	return strings.TrimSpace(html.UnescapeString(tagPattern.ReplaceAllString(s, "")))
}
