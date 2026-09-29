package metadata

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"marquee/internal/catalog"
	"marquee/internal/httpx"
	"marquee/internal/store"
)

// Cache lifetimes. TMDB allows caching for at most six months.
const (
	titleTTL   = 7 * 24 * time.Hour
	episodeTTL = 24 * time.Hour
)

// Service routes requests to providers and caches results in the store.
type Service struct {
	// Log receives provider failures and fallbacks. Defaults to slog.Default().
	Log *slog.Logger
	// PosterCacheDir stores downloaded posters. Empty disables the disk cache.
	PosterCacheDir string

	imageClient *http.Client

	store    *store.Store
	tmdb     *TMDB // nil when no token is configured
	tvmaze   Provider
	byName   map[string]Provider
	tmdbMu   sync.Mutex
	tmdbSeen time.Time
	tmdbStat string
}

// NewService builds the metadata service. tmdb may be nil.
func NewService(st *store.Store, tmdb *TMDB, tvmaze Provider) *Service {
	s := &Service{Log: slog.Default(), imageClient: newHTTPClient(), store: st, tmdb: tmdb, tvmaze: tvmaze,
		byName: map[string]Provider{tvmaze.Name(): tvmaze}}
	if tmdb != nil {
		s.byName[tmdb.Name()] = tmdb
	}
	return s
}

// SearchOutcome is the result of a search, with the provider that answered.
type SearchOutcome struct {
	Source  string
	Results []catalog.SearchResult
	// Notice explains a fallback, e.g. when TMDB failed and TVmaze answered.
	Notice string
}

// Search uses TMDB when configured (transient failures are retried by the
// HTTP layer) and falls back to TVmaze, series only, if TMDB still fails.
func (s *Service) Search(ctx context.Context, query string) (SearchOutcome, error) {
	var notice string
	if s.tmdb != nil {
		results, err := s.tmdb.Search(ctx, query)
		if err == nil {
			return SearchOutcome{Source: s.tmdb.Name(), Results: results}, nil
		}
		if ctx.Err() != nil {
			return SearchOutcome{}, ctx.Err()
		}
		s.Log.Warn("tmdb search failed, falling back to tvmaze", "query", query, "err", err)
		notice = "TMDB search failed (" + httpx.Reason(err) + "); showing TVmaze series only"
	}
	results, err := s.tvmaze.Search(ctx, query)
	if err != nil {
		s.Log.Warn("tvmaze search failed", "query", query, "err", err)
	}
	return SearchOutcome{Source: s.tvmaze.Name(), Results: results, Notice: notice}, err
}

func (s *Service) provider(ref catalog.Ref) (Provider, error) {
	p, ok := s.byName[ref.Source]
	if !ok {
		return nil, fmt.Errorf("%w: no provider for %q", ErrNotFound, ref.Source)
	}
	return p, nil
}

// Title returns a title, from the cache when fresh. If the provider is
// unavailable, a stale cached copy is returned instead of an error.
func (s *Service) Title(ctx context.Context, rawRef string) (catalog.Title, error) {
	ref, err := catalog.ParseRef(rawRef)
	if err != nil {
		return catalog.Title{}, fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	cached, found, err := s.store.GetTitle(ctx, ref.String())
	if err != nil {
		return catalog.Title{}, err
	}
	if found && time.Since(cached.FetchedAt) < titleTTL && cached.Details.Version >= catalog.DetailsVersion {
		return cached, nil
	}
	p, err := s.provider(ref)
	if err != nil {
		return catalog.Title{}, err
	}
	fresh, err := p.Title(ctx, ref)
	if err != nil {
		if found && !errors.Is(err, ErrNotFound) {
			return cached, nil
		}
		return catalog.Title{}, err
	}
	if err := s.store.SaveTitle(ctx, fresh); err != nil {
		return catalog.Title{}, err
	}
	return fresh, nil
}

// Episodes returns the episodes of a season, from the cache when fresh.
func (s *Service) Episodes(ctx context.Context, rawRef string, season int) ([]catalog.Episode, error) {
	ref, err := catalog.ParseRef(rawRef)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	// The series row must exist before its episodes can be stored.
	if _, err := s.Title(ctx, ref.String()); err != nil {
		return nil, err
	}
	cached, fetched, found, err := s.store.GetEpisodes(ctx, ref.String(), season)
	if err != nil {
		return nil, err
	}
	if found && time.Since(fetched) < episodeTTL {
		return cached, nil
	}
	p, err := s.provider(ref)
	if err != nil {
		return nil, err
	}
	fresh, err := p.Episodes(ctx, ref, season)
	if err != nil {
		if found && !errors.Is(err, ErrNotFound) {
			return cached, nil
		}
		return nil, err
	}
	if err := s.store.SaveEpisodes(ctx, ref.String(), season, fresh); err != nil {
		return nil, err
	}
	return fresh, nil
}

// Checks reports the state of each credentialed provider: "valid",
// "invalid", "unreachable" or "not_configured". TMDB is re-checked at most
// once a minute.
func (s *Service) Checks(ctx context.Context) map[string]string {
	checks := map[string]string{"tmdb": s.tmdbState(ctx)}
	if s.tmdb != nil {
		checks["tmdb_format"] = s.tmdb.CredentialFormat()
	}
	return checks
}

func (s *Service) tmdbState(ctx context.Context) string {
	if s.tmdb == nil {
		return "not_configured"
	}
	s.tmdbMu.Lock()
	defer s.tmdbMu.Unlock()
	if s.tmdbStat != "" && (s.tmdbStat != "unreachable" || time.Since(s.tmdbSeen) < time.Minute) {
		return s.tmdbStat
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	switch err := s.tmdb.Validate(ctx); {
	case err == nil:
		s.tmdbStat = "valid"
	case errors.Is(err, ErrUnauthorized):
		s.tmdbStat = "invalid"
	default:
		s.tmdbStat = "unreachable"
	}
	s.tmdbSeen = time.Now()
	return s.tmdbStat
}
