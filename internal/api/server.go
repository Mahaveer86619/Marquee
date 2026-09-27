// Package api exposes the core HTTP API.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"marquee/internal/catalog"
	"marquee/internal/metadata"
	"marquee/internal/version"
)

// Catalog is the metadata service used by the API.
type Catalog interface {
	Search(ctx context.Context, query string) (metadata.SearchOutcome, error)
	Title(ctx context.Context, ref string) (catalog.Title, error)
	Episodes(ctx context.Context, ref string, season int) ([]catalog.Episode, error)
	Poster(ctx context.Context, ref string) ([]byte, string, error)
	Checks(ctx context.Context) map[string]string
}

// Options configures the API.
type Options struct {
	// Providers reports which external providers have credentials configured.
	// Only presence is exposed, never the credential values.
	Providers map[string]bool
	// Catalog serves metadata. When nil, metadata endpoints return 503.
	Catalog Catalog
	// Log receives one line per request (health checks excluded). Optional.
	Log *slog.Logger
}

// New returns the core HTTP handler.
func New(opts Options) http.Handler {
	started := time.Now()
	providers := opts.Providers
	if providers == nil {
		providers = map[string]bool{}
	}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":         "ok",
			"uptime_seconds": int(time.Since(started).Seconds()),
		})
	})

	mux.HandleFunc("GET /api/v1/version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"service": "core",
			"version": version.Version,
			"commit":  version.Commit,
		})
	})

	mux.HandleFunc("GET /api/v1/status", func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{"providers": providers}
		if opts.Catalog != nil {
			body["checks"] = opts.Catalog.Checks(r.Context())
		}
		writeJSON(w, http.StatusOK, body)
	})

	mux.HandleFunc("GET /api/v1/search", func(w http.ResponseWriter, r *http.Request) {
		if !requireCatalog(w, opts.Catalog) {
			return
		}
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			writeError(w, http.StatusBadRequest, "query parameter q is required")
			return
		}
		out, err := opts.Catalog.Search(r.Context(), q)
		if err != nil {
			writeUpstreamError(w, err)
			return
		}
		if out.Results == nil {
			out.Results = []catalog.SearchResult{}
		}
		body := map[string]any{"query": q, "source": out.Source, "results": out.Results}
		if out.Notice != "" {
			body["notice"] = out.Notice
		}
		writeJSON(w, http.StatusOK, body)
	})

	mux.HandleFunc("GET /api/v1/titles/{ref}", func(w http.ResponseWriter, r *http.Request) {
		if !requireCatalog(w, opts.Catalog) {
			return
		}
		title, err := opts.Catalog.Title(r.Context(), r.PathValue("ref"))
		if err != nil {
			writeUpstreamError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, title)
	})

	mux.HandleFunc("GET /api/v1/titles/{ref}/poster", func(w http.ResponseWriter, r *http.Request) {
		if !requireCatalog(w, opts.Catalog) {
			return
		}
		data, ctype, err := opts.Catalog.Poster(r.Context(), r.PathValue("ref"))
		if err != nil {
			writeUpstreamError(w, err)
			return
		}
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Cache-Control", "private, max-age=86400")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	})

	mux.HandleFunc("GET /api/v1/titles/{ref}/seasons/{season}", func(w http.ResponseWriter, r *http.Request) {
		if !requireCatalog(w, opts.Catalog) {
			return
		}
		season, err := strconv.Atoi(r.PathValue("season"))
		if err != nil || season < 0 {
			writeError(w, http.StatusBadRequest, "season must be a non-negative number")
			return
		}
		episodes, err := opts.Catalog.Episodes(r.Context(), r.PathValue("ref"), season)
		if err != nil {
			writeUpstreamError(w, err)
			return
		}
		if episodes == nil {
			episodes = []catalog.Episode{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"ref": r.PathValue("ref"), "season": season, "episodes": episodes})
	})

	if opts.Log == nil {
		return mux
	}
	return accessLog(opts.Log, mux)
}

// statusRecorder captures the response status for the access log.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// accessLog logs method, path, status and duration. Query strings are not
// logged except the search term, and health checks are skipped.
func accessLog(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		attrs := []any{"method", r.Method, "path", r.URL.Path, "status", rec.status, "ms", time.Since(start).Milliseconds()}
		if q := r.URL.Query().Get("q"); q != "" {
			attrs = append(attrs, "q", q)
		}
		level := slog.LevelInfo
		if rec.status >= 500 {
			level = slog.LevelWarn
		}
		log.Log(r.Context(), level, "request", attrs...)
	})
}

func requireCatalog(w http.ResponseWriter, c Catalog) bool {
	if c == nil {
		writeError(w, http.StatusServiceUnavailable, "metadata service is not available")
		return false
	}
	return true
}

func writeUpstreamError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, metadata.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "metadata provider timed out")
	default:
		writeError(w, http.StatusBadGateway, err.Error())
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
