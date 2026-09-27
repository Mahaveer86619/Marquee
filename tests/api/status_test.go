package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"marquee/internal/api"
)

func TestStatusReportsProviderPresenceOnly(t *testing.T) {
	h := api.New(api.Options{Providers: map[string]bool{"tmdb": true, "prowlarr": false}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Providers map[string]bool `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Providers["tmdb"] || body.Providers["prowlarr"] {
		t.Fatalf("providers = %v", body.Providers)
	}
}

func TestStatusWithoutProviders(t *testing.T) {
	rec := httptest.NewRecorder()
	api.New(api.Options{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))
	if !strings.Contains(rec.Body.String(), `"providers":{}`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}
