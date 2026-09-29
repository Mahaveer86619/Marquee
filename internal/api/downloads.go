package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"marquee/internal/download"
)

// Downloads is the download queue used by the API.
type Downloads interface {
	Enqueue(ctx context.Context, r download.Request) (download.Download, error)
	List(ctx context.Context, limit int) ([]download.View, error)
	Get(ctx context.Context, id string) (download.View, error)
	Pause(ctx context.Context, id string) error
	Resume(ctx context.Context, id string) error
	Cancel(ctx context.Context, id string) error
}

// downloadRoutes registers the download endpoints.
func downloadRoutes(mux *http.ServeMux, opts Options) {
	// POST /api/v1/downloads {"release_id","ref","scope","season","episode","audio","subtitles"}
	mux.HandleFunc("POST /api/v1/downloads", func(w http.ResponseWriter, r *http.Request) {
		if !requireDownloads(w, opts) || !requireReleases(w, opts) {
			return
		}
		var body struct {
			ReleaseID string   `json:"release_id"`
			Ref       string   `json:"ref"`
			Scope     string   `json:"scope"`
			Season    int      `json:"season"`
			Episode   int      `json:"episode"`
			Audio     []string `json:"audio"`
			Subtitles []string `json:"subtitles"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if body.ReleaseID == "" {
			writeError(w, http.StatusBadRequest, "release_id is required")
			return
		}
		target, ok := resolveTarget(w, r, opts, body.Ref, body.Scope, strconv.Itoa(body.Season), strconv.Itoa(body.Episode))
		if !ok {
			return
		}
		rel, found, err := opts.Releases.Get(r.Context(), body.ReleaseID)
		switch {
		case err != nil:
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		case !found:
			writeError(w, http.StatusNotFound, "release not found (search results expire after an hour; search again)")
			return
		}
		d, err := opts.Downloads.Enqueue(r.Context(), download.Request{
			Release: rel, Target: target, TitleRef: body.Ref, Audio: body.Audio, Subtitles: body.Subtitles,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, d)
	})

	mux.HandleFunc("GET /api/v1/downloads", func(w http.ResponseWriter, r *http.Request) {
		if !requireDownloads(w, opts) {
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 || limit > 500 {
			limit = 100
		}
		list, err := opts.Downloads.List(r.Context(), limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"downloads": list})
	})

	mux.HandleFunc("GET /api/v1/downloads/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !requireDownloads(w, opts) {
			return
		}
		v, err := opts.Downloads.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			writeDownloadError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, v)
	})

	actions := map[string]func(Downloads, context.Context, string) error{
		"pause":  Downloads.Pause,
		"resume": Downloads.Resume,
		"cancel": Downloads.Cancel,
	}
	for name, act := range actions {
		mux.HandleFunc("POST /api/v1/downloads/{id}/"+name, func(w http.ResponseWriter, r *http.Request) {
			if !requireDownloads(w, opts) {
				return
			}
			id := r.PathValue("id")
			if err := act(opts.Downloads, r.Context(), id); err != nil {
				writeDownloadError(w, err)
				return
			}
			v, err := opts.Downloads.Get(r.Context(), id)
			if err != nil {
				writeDownloadError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, v)
		})
	}
}

func requireDownloads(w http.ResponseWriter, opts Options) bool {
	if opts.Downloads == nil {
		writeError(w, http.StatusServiceUnavailable, "downloads are not available")
		return false
	}
	return true
}

func writeDownloadError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, download.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, download.ErrInvalidState):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
