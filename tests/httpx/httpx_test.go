package httpx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"marquee/internal/httpx"
)

// TestTimeoutIsNotRetried: a slow service must not receive the same request
// again after the client gives up (it would repeat the slow work).
func TestTimeoutIsNotRetried(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	var out map[string]any
	err := httpx.GetJSON(context.Background(), httpx.NewClientWithTimeout(50*time.Millisecond), srv.URL, nil, &out)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("server received %d requests, want 1 (timeouts are not retried)", n)
	}
}

func TestRedact(t *testing.T) {
	if got := httpx.Redact("https://api.example.com/3/movie/1?api_key=secret&language=en"); got != "https://api.example.com/3/movie/1?[redacted]" {
		t.Fatalf("Redact = %q", got)
	}
}
