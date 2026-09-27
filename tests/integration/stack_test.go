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

func TestCoreVersion(t *testing.T) {
	if got := getJSON(t, "/api/v1/version")["service"]; got != "core" {
		t.Fatalf("service = %v, want core", got)
	}
}
