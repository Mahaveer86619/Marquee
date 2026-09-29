package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"marquee/internal/release"
)

// Releases is the release search service used by the API.
type Releases interface {
	Search(ctx context.Context, t release.Target, refresh bool, source string) (release.Result, error)
	Sources() []release.SourceInfo
	Get(ctx context.Context, id string) (release.Release, bool, error)
	AddMagnet(ctx context.Context, t release.Target, link string) (release.Release, error)
	Prefs() release.Prefs
}

// releaseRoutes registers the release endpoints.
func releaseRoutes(mux *http.ServeMux, opts Options) {
	// GET /api/v1/releases?ref=<title ref>&scope=movie|episode|season|series&season=N&episode=N[&refresh=1]
	mux.HandleFunc("GET /api/v1/releases", func(w http.ResponseWriter, r *http.Request) {
		if !requireReleases(w, opts) {
			return
		}
		q := r.URL.Query()
		target, ok := resolveTarget(w, r, opts, q.Get("ref"), q.Get("scope"), q.Get("season"), q.Get("episode"))
		if !ok {
			return
		}
		res, err := opts.Releases.Search(r.Context(), target, q.Get("refresh") == "1", q.Get("source"))
		if err != nil {
			writeUpstreamError(w, err)
			return
		}
		if res.Releases == nil {
			res.Releases = []release.Release{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"target": res.Target, "releases": res.Releases, "sources": res.Sources,
			"cached": res.Cached, "fetched_at": res.FetchedAt, "preferences": preferences(opts.Releases.Prefs()),
		})
	})

	mux.HandleFunc("GET /api/v1/releases/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !requireReleases(w, opts) {
			return
		}
		rel, found, err := opts.Releases.Get(r.Context(), r.PathValue("id"))
		switch {
		case err != nil:
			writeError(w, http.StatusInternalServerError, err.Error())
		case !found:
			writeError(w, http.StatusNotFound, "release not found (search results expire after an hour)")
		default:
			writeJSON(w, http.StatusOK, rel)
		}
	})

	// POST /api/v1/releases/magnet {"ref","scope","season","episode","magnet"}
	mux.HandleFunc("POST /api/v1/releases/magnet", func(w http.ResponseWriter, r *http.Request) {
		if !requireReleases(w, opts) {
			return
		}
		var body struct {
			Ref     string `json:"ref"`
			Scope   string `json:"scope"`
			Season  int    `json:"season"`
			Episode int    `json:"episode"`
			Magnet  string `json:"magnet"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		target, ok := resolveTarget(w, r, opts, body.Ref, body.Scope, strconv.Itoa(body.Season), strconv.Itoa(body.Episode))
		if !ok {
			return
		}
		rel, err := opts.Releases.AddMagnet(r.Context(), target, body.Magnet)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, rel)
	})
}

// resolveTarget loads the title and builds the search target, writing an
// error response when that fails.
func resolveTarget(w http.ResponseWriter, r *http.Request, opts Options, ref, scope, season, episode string) (release.Target, bool) {
	if ref == "" || scope == "" {
		writeError(w, http.StatusBadRequest, "ref and scope are required")
		return release.Target{}, false
	}
	se, _ := strconv.Atoi(season)
	ep, _ := strconv.Atoi(episode)
	title, err := opts.Catalog.Title(r.Context(), ref)
	if err != nil {
		writeUpstreamError(w, err)
		return release.Target{}, false
	}
	target, err := release.TargetFor(title, release.Scope(scope), se, ep)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return release.Target{}, false
	}
	return target, true
}

func requireReleases(w http.ResponseWriter, opts Options) bool {
	if opts.Releases == nil || opts.Catalog == nil {
		writeError(w, http.StatusServiceUnavailable, "release search is not available")
		return false
	}
	return true
}

// preferences reports the ranking preferences as ISO 639-1 codes.
func preferences(p release.Prefs) map[string][]string {
	conv := func(in []string) []string {
		out := make([]string, 0, len(in))
		for _, c := range in {
			out = append(out, release.ToISO1(c))
		}
		return out
	}
	return map[string][]string{"audio_languages": conv(p.AudioLanguages), "subtitle_languages": conv(p.SubtitleLanguages)}
}
