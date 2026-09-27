package launcher

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFailed(t *testing.T) {
	if Failed([]Result{{Status: OK}, {Status: Warn}}) {
		t.Fatal("warnings must not count as failures")
	}
	if !Failed([]Result{{Status: OK}, {Status: Fail}}) {
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

	if r := checkCore(context.Background(), srv.URL); r.Status != OK {
		t.Fatalf("status = %s, detail = %s", r.Status, r.Detail)
	}
	if r := checkCore(context.Background(), "http://127.0.0.1:1"); r.Status != Warn {
		t.Fatalf("unreachable core should warn, got %s", r.Status)
	}
}

func TestPrint(t *testing.T) {
	var buf bytes.Buffer
	Print(&buf, []Result{{Name: "docker", Status: OK, Detail: "found"}})
	if !strings.Contains(buf.String(), "[ok  ] docker") {
		t.Fatalf("unexpected output: %q", buf.String())
	}
}
