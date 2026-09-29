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
	"strconv"
	"syscall"
	"time"

	"marquee/internal/api"
	"marquee/internal/config"
	"marquee/internal/download"
	"marquee/internal/download/anacrolix"
	"marquee/internal/metadata"
	"marquee/internal/release"
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

	releases := releaseService(st, settings, log)

	downloads, engine, err := downloadManager(ctx, st, catalogSvc, settings, log)
	if err != nil {
		log.Error("cannot start downloads", "err", err)
		os.Exit(1)
	}
	defer engine.Close()
	defer downloads.Close()

	providers := configuredProviders()
	srv := &http.Server{
		Addr:              *addr,
		Handler:           api.New(api.Options{Providers: providers, Catalog: catalogSvc, Releases: releases, Downloads: downloads, Log: log}),
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

// releaseService sets up release sources: the Internet Archive always, and
// Prowlarr when it is enabled in config.json and its API key is set.
func releaseService(st *store.Store, settings config.Config, log *slog.Logger) *release.Service {
	sources := []release.Source{release.NewInternetArchive()}
	unconfigured := map[string]string{}
	key := os.Getenv("MARQUEE_PROWLARR_API_KEY")
	switch {
	case settings.Indexers.Prowlarr.Enabled && key != "":
		sources = append(sources, release.NewProwlarr(settings.Indexers.Prowlarr.URL, key))
	case !settings.Indexers.Prowlarr.Enabled:
		unconfigured["prowlarr"] = "not enabled (run setup with -WithIndexers)"
	default:
		unconfigured["prowlarr"] = "enabled, but MARQUEE_PROWLARR_API_KEY is not set in deploy/.env"
	}
	prefs := release.Prefs{
		AudioLanguages:    settings.Playback.AudioLanguages,
		SubtitleLanguages: settings.Playback.SubtitleLanguages,
	}
	svc := release.NewService(st, prefs, unconfigured, sources...)
	svc.Log = log
	return svc
}

// downloadManager starts the torrent engine and the download queue.
// In-progress data lives in MARQUEE_DOWNLOADS_DIR; finished files move to
// MARQUEE_LIBRARY_DIR.
func downloadManager(ctx context.Context, st *store.Store, cat download.Catalog, settings config.Config, log *slog.Logger) (*download.Manager, *anacrolix.Engine, error) {
	port := settings.Downloads.TorrentPort
	if v, err := strconv.Atoi(os.Getenv("MARQUEE_TORRENT_PORT")); err == nil && v > 0 {
		port = v // the published port wins, so peers reach the port we listen on
	}
	if port <= 0 || port > 65535 {
		port = config.DefaultTorrentPort
	}
	dir := envOr("MARQUEE_DOWNLOADS_DIR", "downloads")
	engine, err := anacrolix.New(dir, port, log.With("component", "torrent"))
	if err != nil {
		return nil, nil, err
	}
	m := download.NewManager(download.Options{
		Engine:     engine,
		Store:      st,
		Catalog:    cat,
		LibraryDir: envOr("MARQUEE_LIBRARY_DIR", "library"),
		MaxActive:  settings.Downloads.MaxActive,
		NewID:      store.NewDownloadID,
		Log:        log,
	})
	if err := m.Start(ctx); err != nil {
		engine.Close()
		return nil, nil, err
	}
	log.Info("downloads ready", "dir", dir, "torrent_port", port, "max_active", settings.Downloads.MaxActive)
	return m, engine, nil
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
