// Package api exposes the core HTTP API.
package api

import (
	"encoding/json"
	"net/http"
	"time"

	"marquee/internal/version"
)

// New returns the core HTTP handler.
func New() http.Handler {
	started := time.Now()
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

	return mux
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
