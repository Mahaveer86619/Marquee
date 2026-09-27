//go:build integration

// Package integration tests a running stack. Start it first (scripts/full-up
// or `marquee up`), then run: go test -tags integration ./tests/integration
package integration

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"
)

func coreURL() string {
	if u := os.Getenv("MARQUEE_CORE_URL"); u != "" {
		return u
	}
	return "http://127.0.0.1:7700"
}

func getJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(coreURL() + path)
	if err != nil {
		t.Fatalf("core not reachable (is the stack running?): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", path, resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("GET %s: invalid JSON: %v", path, err)
	}
	return body
}

func TestCoreHealthy(t *testing.T) {
	if got := getJSON(t, "/healthz")["status"]; got != "ok" {
		t.Fatalf("status = %v, want ok", got)
	}
}

func TestCoreStatusListsProviders(t *testing.T) {
	providers, ok := getJSON(t, "/api/v1/status")["providers"].(map[string]any)
	if !ok {
		t.Fatal("providers missing from /api/v1/status")
	}
	for _, name := range []string{"tmdb", "prowlarr"} {
		if _, ok := providers[name].(bool); !ok {
			t.Fatalf("provider %s missing or not a boolean", name)
		}
	}
}

// TestSearchTitleAndSeason needs internet access (TMDB or TVmaze).
func TestSearchTitleAndSeason(t *testing.T) {
	search := getJSON(t, "/api/v1/search?q=sherlock")
	results, _ := search["results"].([]any)
	if len(results) == 0 {
		t.Fatalf("no search results (source %v)", search["source"])
	}
	var ref string
	for _, r := range results {
		if m, _ := r.(map[string]any); m["kind"] == "series" {
			ref, _ = m["ref"].(string)
			break
		}
	}
	if ref == "" {
		t.Fatal("no series in the search results")
	}
	title := getJSON(t, "/api/v1/titles/"+ref)
	if seasons, _ := title["seasons"].([]any); len(seasons) == 0 {
		t.Fatalf("title %s has no seasons", ref)
	}
	episodes, _ := getJSON(t, "/api/v1/titles/"+ref+"/seasons/1")["episodes"].([]any)
	if len(episodes) == 0 {
		t.Fatalf("season 1 of %s has no episodes", ref)
	}
}

func TestCoreVersion(t *testing.T) {
	if got := getJSON(t, "/api/v1/version")["service"]; got != "core" {
		t.Fatalf("service = %v, want core", got)
	}
}
