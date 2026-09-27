// Package api exposes the core HTTP API.
package api

import (
	"encoding/json"
	"net/http"
	"time"

	"marquee/internal/version"
)

// Options configures the API.
type Options struct {
	// Providers reports which external providers have credentials configured.
	// Only presence is exposed, never the credential values.
	Providers map[string]bool
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

	mux.HandleFunc("GET /api/v1/status", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"providers": providers})
	})

	return mux
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
