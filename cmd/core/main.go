// Command marquee-core is the core service. It runs inside Docker.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"marquee/internal/api"
	"marquee/internal/version"
)

func main() {
	addr := flag.String("addr", envOr("MARQUEE_CORE_ADDR", ":7700"), "listen address")
	healthcheck := flag.Bool("healthcheck", false, "probe the local health endpoint and exit (used by Docker)")
	flag.Parse()

	if *healthcheck {
		os.Exit(probe(*addr))
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	providers := configuredProviders()
	srv := &http.Server{
		Addr:              *addr,
		Handler:           api.New(api.Options{Providers: providers}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Info("core starting", "addr", *addr, "version", version.String(), "providers", providers)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server failed", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown failed", "err", err)
	}
	log.Info("core stopped")
}

func probe(addr string) int {
	host := addr
	if len(host) > 0 && host[0] == ':' {
		host = "127.0.0.1" + host
	}
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + host + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

// configuredProviders reports which provider credentials were supplied through
// the environment (deploy/.env). Values are never logged or exposed.
func configuredProviders() map[string]bool {
	return map[string]bool{
		"tmdb":          os.Getenv("MARQUEE_TMDB_TOKEN") != "",
		"prowlarr":      os.Getenv("MARQUEE_PROWLARR_API_KEY") != "",
		"opensubtitles": os.Getenv("MARQUEE_OPENSUBTITLES_API_KEY") != "",
		"subdl":         os.Getenv("MARQUEE_SUBDL_API_KEY") != "",
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
