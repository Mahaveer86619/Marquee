// Command marquee-agent is the host bridge. It will control mpv, open the web
// player, detect drives and run local backups. For now it serves a health
// endpoint and verifies it can reach the core service.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"time"

	"marquee/internal/version"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:7701", "listen address (loopback only)")
	coreURL := flag.String("core", "http://127.0.0.1:7700", "core service URL")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	if path, err := exec.LookPath("mpv"); err != nil {
		log.Warn("mpv not found on PATH; playback will be unavailable")
	} else {
		log.Info("mpv found", "path", path)
	}
	if err := ping(*coreURL); err != nil {
		log.Warn("core not reachable yet", "url", *coreURL, "err", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	srv := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	go func() {
		log.Info("agent starting", "addr", *addr, "version", version.String())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("agent failed", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

func ping(coreURL string) error {
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(coreURL + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New(resp.Status)
	}
	return nil
}
