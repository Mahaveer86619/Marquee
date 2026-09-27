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
	"path/filepath"
	"syscall"
	"time"

	"marquee/internal/api"
	"marquee/internal/config"
	"marquee/internal/metadata"
	"marquee/internal/store"
	"marquee/internal/version"
)

func main() {
	addr := flag.String("addr", envOr("MARQUEE_CORE_ADDR", ":7700"), "listen address")
	healthcheck := flag.Bool("healthcheck", false, "probe the local health endpoint and exit (used by Docker)")
	flag.Parse()

	if *healthcheck {
		os.Exit(probe(*addr))
	}

	level := slog.LevelInfo
	if os.Getenv("MARQUEE_LOG_SQL") == "1" || os.Getenv("MARQUEE_DEBUG") == "1" {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dataDir := envOr("MARQUEE_DATA_DIR", "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Error("cannot create data folder", "dir", dataDir, "err", err)
		os.Exit(1)
	}
	var storeOpts []store.Option
	if os.Getenv("MARQUEE_LOG_SQL") == "1" {
		storeOpts = append(storeOpts, store.WithQueryLog(log))
	}
	st, err := store.Open(ctx, filepath.Join(dataDir, "core.db"), storeOpts...)
	if err != nil {
		log.Error("cannot open database", "err", err)
		os.Exit(1)
	}
	defer st.Close()
	schema, _ := st.SchemaVersion(ctx)

	settings := loadSettings(log)
	var tmdb *metadata.TMDB
	if cred := tmdbCredential(); cred != "" {
		tmdb = metadata.NewTMDB(cred, settings.Metadata.Language)
		go tmdb.KeepWarm(ctx, 45*time.Second)
	}
	catalogSvc := metadata.NewService(st, tmdb, metadata.NewTVmaze())
	catalogSvc.Log = log
	catalogSvc.PosterCacheDir = filepath.Join(envOr("MARQUEE_CACHE_DIR", "cache"), "posters")

	providers := configuredProviders()
	srv := &http.Server{
		Addr:              *addr,
		Handler:           api.New(api.Options{Providers: providers, Catalog: catalogSvc, Log: log}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Info("core starting", "addr", *addr, "version", version.String(), "schema", schema,
			"language", settings.Metadata.Language, "providers", providers)
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

// loadSettings reads config.json from the mounted data folder. Missing or
// unreadable settings fall back to defaults.
func loadSettings(log *slog.Logger) config.Config {
	file := envOr("MARQUEE_CONFIG_FILE", "")
	if file == "" {
		return config.Default("")
	}
	cfg, _, err := config.Load(filepath.Dir(file))
	if err != nil {
		log.Warn("using default settings", "err", err)
		return config.Default("")
	}
	return cfg
}

// configuredProviders reports which provider credentials were supplied through
// the environment (deploy/.env). Values are never logged or exposed.
func configuredProviders() map[string]bool {
	return map[string]bool{
		"tmdb":          tmdbCredential() != "",
		"prowlarr":      os.Getenv("MARQUEE_PROWLARR_API_KEY") != "",
		"opensubtitles": os.Getenv("MARQUEE_OPENSUBTITLES_API_KEY") != "",
		"subdl":         os.Getenv("MARQUEE_SUBDL_API_KEY") != "",
	}
}

// tmdbCredential prefers the Read Access Token and falls back to the API key.
func tmdbCredential() string {
	if v := os.Getenv("MARQUEE_TMDB_READ_ACCESS_TOKEN"); v != "" {
		return v
	}
	return os.Getenv("MARQUEE_TMDB_TOKEN")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
