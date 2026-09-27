package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"marquee/internal/api"
	"marquee/internal/catalog"
	"marquee/internal/metadata"
)

type fakeCatalog struct{ lastQuery string }

func (f *fakeCatalog) Search(_ context.Context, q string) (metadata.SearchOutcome, error) {
	f.lastQuery = q
	return metadata.SearchOutcome{
		Source:  "tvmaze",
		Results: []catalog.SearchResult{{Ref: "tvmaze:show:82", Kind: catalog.Series, Name: "Example Show"}},
		Notice:  "TMDB search failed (timed out); showing TVmaze series only",
	}, nil
}

func (f *fakeCatalog) Title(_ context.Context, ref string) (catalog.Title, error) {
	if ref != "tvmaze:show:82" {
		return catalog.Title{}, fmt.Errorf("%w: %s", metadata.ErrNotFound, ref)
	}
	return catalog.Title{Ref: ref, Kind: catalog.Series, Name: "Example Show"}, nil
}

func (f *fakeCatalog) Episodes(_ context.Context, ref string, season int) ([]catalog.Episode, error) {
	return []catalog.Episode{{Ref: catalog.EpisodeRef(ref, season, 1), Season: season, Number: 1}}, nil
}

func (f *fakeCatalog) Poster(_ context.Context, ref string) ([]byte, string, error) {
	if ref != "tvmaze:show:82" {
		return nil, "", fmt.Errorf("%w: no poster", metadata.ErrNotFound)
	}
	return []byte("\x89PNG\r\n\x1a\n"), "image/png", nil
}

func TestPosterEndpoint(t *testing.T) {
	h := api.New(api.Options{Catalog: &fakeCatalog{}})
	rec := do(t, h, "/api/v1/titles/tvmaze:show:82/poster")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("status=%d type=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := do(t, h, "/api/v1/titles/tvmaze:show:1/poster"); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func (f *fakeCatalog) Checks(context.Context) map[string]string {
	return map[string]string{"tmdb": "not_configured"}
}

func do(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestSearchEndpoint(t *testing.T) {
	fc := &fakeCatalog{}
	rec := do(t, api.New(api.Options{Catalog: fc}), "/api/v1/search?q=%20example%20")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Source  string                 `json:"source"`
		Results []catalog.SearchResult `json:"results"`
		Notice  string                 `json:"notice"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if fc.lastQuery != "example" || body.Source != "tvmaze" || len(body.Results) != 1 {
		t.Fatalf("query=%q body=%+v", fc.lastQuery, body)
	}
	if body.Notice == "" {
		t.Fatal("fallback notice not passed through")
	}
}

func TestSearchRequiresQuery(t *testing.T) {
	if rec := do(t, api.New(api.Options{Catalog: &fakeCatalog{}}), "/api/v1/search?q="); rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestTitleEndpointAndNotFound(t *testing.T) {
	h := api.New(api.Options{Catalog: &fakeCatalog{}})
	if rec := do(t, h, "/api/v1/titles/tvmaze:show:82"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, h, "/api/v1/titles/tvmaze:show:1"); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestSeasonEndpoint(t *testing.T) {
	h := api.New(api.Options{Catalog: &fakeCatalog{}})
	rec := do(t, h, "/api/v1/titles/tvmaze:show:82/seasons/2")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Episodes []catalog.Episode `json:"episodes"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.Episodes) != 1 || body.Episodes[0].Ref != "tvmaze:show:82:s02e01" {
		t.Fatalf("episodes = %+v", body.Episodes)
	}
	if rec := do(t, h, "/api/v1/titles/tvmaze:show:82/seasons/x"); rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestMetadataUnavailableWithoutCatalog(t *testing.T) {
	if rec := do(t, api.New(api.Options{}), "/api/v1/search?q=x"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestStatusIncludesChecks(t *testing.T) {
	rec := do(t, api.New(api.Options{Catalog: &fakeCatalog{}}), "/api/v1/status")
	var body struct {
		Checks map[string]string `json:"checks"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Checks["tmdb"] != "not_configured" {
		t.Fatalf("checks = %v", body.Checks)
	}
}
