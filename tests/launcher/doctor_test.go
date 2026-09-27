package launcher_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"marquee/internal/launcher"
)

func TestFailed(t *testing.T) {
	if launcher.Failed([]launcher.Result{{Status: launcher.OK}, {Status: launcher.Warn}}) {
		t.Fatal("warnings must not count as failures")
	}
	if !launcher.Failed([]launcher.Result{{Status: launcher.OK}, {Status: launcher.Fail}}) {
		t.Fatal("expected failure")
	}
}

func TestCheckCore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if r := launcher.CheckCore(context.Background(), srv.URL); r.Status != launcher.OK {
		t.Fatalf("status = %s, detail = %s", r.Status, r.Detail)
	}
	if r := launcher.CheckCore(context.Background(), "http://127.0.0.1:1"); r.Status != launcher.Warn {
		t.Fatalf("unreachable core should warn, got %s", r.Status)
	}
}

func TestCheckProviders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"providers":{"tmdb":true,"prowlarr":false,"subdl":true},"checks":{"tmdb":"valid","subdl":"invalid"}}`))
	}))
	defer srv.Close()

	byName := map[string]launcher.Result{}
	for _, r := range launcher.CheckProviders(context.Background(), srv.URL) {
		byName[r.Name] = r
	}
	if r := byName["tmdb key"]; r.Status != launcher.OK || !strings.Contains(r.Detail, "accepted") {
		t.Fatalf("tmdb key = %+v, want ok and accepted", r)
	}
	if byName["prowlarr key"].Status != launcher.Warn {
		t.Fatalf("prowlarr key = %s, want warn", byName["prowlarr key"].Status)
	}
	if r := byName["subdl key"]; r.Status != launcher.Warn || !strings.Contains(r.Detail, "rejected") {
		t.Fatalf("subdl key = %+v, want warn and rejected", r)
	}
}

func TestPrint(t *testing.T) {
	var buf bytes.Buffer
	launcher.Print(&buf, []launcher.Result{{Name: "docker", Status: launcher.OK, Detail: "found"}})
	if !strings.Contains(buf.String(), "[ok  ] docker") {
		t.Fatalf("unexpected output: %q", buf.String())
	}
}
