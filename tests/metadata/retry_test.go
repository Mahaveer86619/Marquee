package metadata_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"marquee/internal/metadata"
)

// TestConnectionDropsAreRetried simulates a network that cuts new connections
// before a response: the first attempts are closed without an HTTP reply.
func TestConnectionDropsAreRetried(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) <= 3 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
			return
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	if err := newTMDB(srv.URL).Validate(context.Background()); err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if n := hits.Load(); n != 4 {
		t.Fatalf("server hit %d times, want 4 (3 drops + success)", n)
	}
}

// TestManyConnectionDropsAreRetried covers networks that drop most new
// connections: 10 drops in a row still end in success.
func TestManyConnectionDropsAreRetried(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) <= 10 {
			if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
				conn.Close()
			}
			return
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()
	if err := newTMDB(srv.URL).Validate(context.Background()); err != nil {
		t.Fatalf("expected success after 10 drops, got %v", err)
	}
}

func TestNotFoundIsNotRetried(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"success":false,"status_code":34,"status_message":"The resource you requested could not be found."}`))
	}))
	defer srv.Close()

	_, err := newTMDB(srv.URL).Search(context.Background(), "x")
	if !errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if !strings.Contains(err.Error(), "code 34") {
		t.Fatalf("TMDB status message not surfaced: %v", err)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("server hit %d times, want 1 (no retry on 404)", n)
	}
}

func TestRateLimitHonoursRetryAfter(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"status_code":25,"status_message":"Your request count is over the allowed limit."}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	if err := newTMDB(srv.URL).Validate(context.Background()); err != nil {
		t.Fatalf("expected success after a 429 retry, got %v", err)
	}
	if n := hits.Load(); n != 2 {
		t.Fatalf("server hit %d times, want 2", n)
	}
}

func TestPersistentRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	if err := newTMDB(srv.URL).Validate(context.Background()); !errors.Is(err, metadata.ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
}
