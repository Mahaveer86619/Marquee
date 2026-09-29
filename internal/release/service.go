package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"marquee/internal/catalog"
	"marquee/internal/httpx"
)

// cacheTTL is how long search results are reused before searching again.
const cacheTTL = time.Hour

// sourceTimeout bounds each source's search.
const sourceTimeout = 25 * time.Second

// Store persists candidates so a release can be chosen by ID later.
type Store interface {
	ReplaceReleases(ctx context.Context, targetKey string, releases []Release, fetched time.Time) error
	ListReleases(ctx context.Context, targetKey string) ([]Release, time.Time, error)
	GetRelease(ctx context.Context, id string) (Release, bool, error)
	SaveRelease(ctx context.Context, targetKey string, r Release) error
}

// SourceStatus reports what each source did for a search.
type SourceStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok, error, not_configured, not_applicable
	Count  int    `json:"count"`
	Detail string `json:"detail,omitempty"`
	Ms     int64  `json:"ms,omitempty"` // how long the source took
}

// Result is a completed search.
type Result struct {
	Target    Target         `json:"target"`
	Releases  []Release      `json:"releases"`
	Sources   []SourceStatus `json:"sources"`
	Cached    bool           `json:"cached"`
	FetchedAt time.Time      `json:"fetched_at"`
}

// Service searches the configured sources.
type Service struct {
	Log     *slog.Logger
	store   Store
	sources []Source
	prefs   Prefs
	// unconfigured lists sources that exist but lack settings, for status output.
	unconfigured map[string]string
}

// NewService returns the release service. unconfigured maps a source name to
// the reason it is off (e.g. Prowlarr without an API key).
func NewService(st Store, prefs Prefs, unconfigured map[string]string, sources ...Source) *Service {
	return &Service{Log: slog.Default(), store: st, sources: sources, prefs: prefs, unconfigured: unconfigured}
}

// Prefs returns the ranking preferences.
func (s *Service) Prefs() Prefs { return s.prefs }

// TargetFor builds a search target from a title and scope.
func TargetFor(t catalog.Title, scope Scope, season, episode int) (Target, error) {
	target := Target{Ref: t.Ref, Scope: scope, Kind: t.Kind, Title: t.Name, Year: t.Year, IMDbID: t.IMDbID, TMDBID: t.TMDBID, TVDBID: t.TVDBID}
	switch scope {
	case ScopeMovie:
		if t.Kind != catalog.Movie {
			return Target{}, errors.New("scope movie needs a film")
		}
	case ScopeSeries:
		if t.Kind != catalog.Series {
			return Target{}, errors.New("scope series needs a series")
		}
	case ScopeSeason:
		if t.Kind != catalog.Series || season < 0 {
			return Target{}, errors.New("scope season needs a series and a season number")
		}
		target.Season = season
		target.Ref = fmt.Sprintf("%s:s%02d", t.Ref, season)
	case ScopeEpisode:
		if t.Kind != catalog.Series || season < 0 || episode < 1 {
			return Target{}, errors.New("scope episode needs a series, season and episode number")
		}
		target.Season, target.Episode = season, episode
		target.Ref = catalog.EpisodeRef(t.Ref, season, episode)
	default:
		return Target{}, errors.New("unknown scope")
	}
	return target, nil
}

// SourceInfo describes a release source for clients that query sources one
// at a time (progressive results).
type SourceInfo struct {
	Name       string `json:"name"`
	Configured bool   `json:"configured"`
	Detail     string `json:"detail,omitempty"`
}

// Sources lists the configured and unconfigured release sources.
func (s *Service) Sources() []SourceInfo {
	out := make([]SourceInfo, 0, len(s.sources)+len(s.unconfigured))
	for _, src := range s.sources {
		out = append(out, SourceInfo{Name: src.Name(), Configured: true})
	}
	for name, why := range s.unconfigured {
		out = append(out, SourceInfo{Name: name, Detail: why})
	}
	return out
}

// Search returns ranked releases for the target, from the cache when fresh.
// When only is set, just that source is searched (clients call once per
// source to show fast results before slow ones).
func (s *Service) Search(ctx context.Context, t Target, refresh bool, only string) (Result, error) {
	cacheKey := t.Key()
	if only != "" {
		cacheKey += "|" + only
	}
	if !refresh {
		if cached, fetched, err := s.store.ListReleases(ctx, cacheKey); err == nil && !fetched.IsZero() && time.Since(fetched) < cacheTTL {
			return Result{Target: t, Releases: cached, Sources: s.cachedStatus(cached, only), Cached: true, FetchedAt: fetched}, nil
		}
	}

	sources := s.sources
	if only != "" {
		sources = nil
		for _, src := range s.sources {
			if src.Name() == only {
				sources = append(sources, src)
			}
		}
	}

	type outcome struct {
		status   SourceStatus
		releases []Release
	}
	outcomes := make([]outcome, len(sources))
	var wg sync.WaitGroup
	for i, src := range sources {
		if !src.Supports(t) {
			outcomes[i].status = SourceStatus{Name: src.Name(), Status: "not_applicable", Detail: notApplicable(src, t)}
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			limit := sourceTimeout
			if tl, ok := src.(interface{ Timeout() time.Duration }); ok {
				limit = tl.Timeout()
			}
			sctx, cancel := context.WithTimeout(ctx, limit)
			defer cancel()
			start := time.Now()
			found, err := src.Search(sctx, t)
			ms := time.Since(start).Milliseconds()
			if err != nil {
				s.Log.Warn("release source failed", "source", src.Name(), "target", t.Key(), "ms", ms, "err", err)
				outcomes[i].status = SourceStatus{Name: src.Name(), Status: "error", Detail: httpx.Reason(err), Ms: ms}
				return
			}
			s.Log.Info("release source done", "source", src.Name(), "target", t.Key(), "ms", ms, "results", len(found))
			outcomes[i] = outcome{status: SourceStatus{Name: src.Name(), Status: "ok", Ms: ms}, releases: found}
		}()
	}
	wg.Wait()

	var all []Release
	statuses := make([]SourceStatus, 0, len(outcomes)+len(s.unconfigured))
	for _, o := range outcomes {
		kept := s.rank(o.releases, t)
		o.status.Count = len(kept)
		statuses = append(statuses, o.status)
		all = append(all, kept...)
	}
	for name, why := range s.unconfigured {
		if only == "" || only == name {
			statuses = append(statuses, SourceStatus{Name: name, Status: "not_configured", Detail: why})
		}
	}

	all = dedupe(all)
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Score != all[j].Score {
			return all[i].Score > all[j].Score
		}
		return all[i].Seeders > all[j].Seeders
	})
	now := time.Now()
	for i := range all {
		all[i].ID = stableID(t.Key(), all[i])
	}
	if err := s.store.ReplaceReleases(ctx, cacheKey, all, now); err != nil {
		return Result{}, err
	}
	return Result{Target: t, Releases: all, Sources: statuses, FetchedAt: now}, nil
}

// rank parses (when needed), drops releases that don't match the target and
// scores the rest.
func (s *Service) rank(found []Release, t Target) []Release {
	var kept []Release
	for _, r := range found {
		if r.Parsed.Title == "" {
			p := ParseName(r.Name)
			if r.Parsed.Subtitles != nil { // source-provided extras win
				p.Subtitles = r.Parsed.Subtitles
			}
			r.Parsed = p
		}
		if !MatchesRelease(r, t) {
			continue
		}
		Score(&r, t, s.prefs)
		kept = append(kept, r)
	}
	return kept
}

// Get returns a stored release by ID.
func (s *Service) Get(ctx context.Context, id string) (Release, bool, error) {
	return s.store.GetRelease(ctx, id)
}

// AddMagnet stores a pasted magnet link as a candidate for the target. It is
// kept even if its name does not parse as the target, since the user chose it.
func (s *Service) AddMagnet(ctx context.Context, t Target, link string) (Release, error) {
	r, err := ParseMagnet(link)
	if err != nil {
		return Release{}, err
	}
	r.Parsed = ParseName(r.Name)
	Score(&r, t, s.prefs)
	r.ID = stableID(t.Key(), r)
	return r, s.store.SaveRelease(ctx, t.Key(), r)
}

// stableID derives a release id from the target and the torrent's identity, so
// the same release keeps its id when a search is refreshed.
func stableID(targetKey string, r Release) string {
	identity := r.InfoHash
	if identity == "" {
		identity = r.Source + "|" + r.Name + "|" + r.TorrentURL
	}
	sum := sha256.Sum256([]byte(targetKey + "|" + identity))
	return hex.EncodeToString(sum[:16])
}

// dedupe drops repeats of the same torrent, keeping the higher-scored copy.
func dedupe(rs []Release) []Release {
	best := map[string]int{}
	var out []Release
	for _, r := range rs {
		key := r.InfoHash
		if key == "" {
			key = r.Source + "|" + r.Name + "|" + r.TorrentURL
		}
		if i, ok := best[key]; ok {
			if r.Score > out[i].Score {
				out[i] = r
			}
			continue
		}
		best[key] = len(out)
		out = append(out, r)
	}
	return out
}

func notApplicable(src Source, t Target) string {
	if src.Name() == "internet_archive" && t.Scope != ScopeMovie {
		return "public-domain films only"
	}
	return "not available for this search"
}

func (s *Service) cachedStatus(rs []Release, only string) []SourceStatus {
	counts := map[string]int{}
	for _, r := range rs {
		counts[r.Source]++
	}
	var out []SourceStatus
	for _, src := range s.sources {
		if only == "" || only == src.Name() {
			out = append(out, SourceStatus{Name: src.Name(), Status: "cached", Count: counts[src.Name()]})
		}
	}
	for name, why := range s.unconfigured {
		if only == "" || only == name {
			out = append(out, SourceStatus{Name: name, Status: "not_configured", Detail: why})
		}
	}
	return out
}
