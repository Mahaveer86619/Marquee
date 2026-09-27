package launcher

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// SetupOptions configures a first-time or repeated setup run.
type SetupOptions struct {
	ComposeFile  string
	CoreURL      string
	MediaDir     string // optional; persisted to the compose .env file
	WithIndexers bool   // also run Prowlarr (compose profile "indexers"); persisted
	NoCache      bool   // rebuild images without the build cache
	Out          io.Writer
}

// Setup prepares the environment, builds the images, starts the stack and
// verifies that every service is healthy. It is safe to run repeatedly.
func Setup(ctx context.Context, opts SetupOptions) error {
	out := opts.Out
	total := 4
	if opts.MediaDir != "" || opts.WithIndexers {
		total = 5
	}
	envFile := filepath.Join(filepath.Dir(opts.ComposeFile), ".env")
	n := 0
	step := func(msg string) {
		n++
		fmt.Fprintf(out, "\n[%d/%d] %s\n", n, total, msg)
	}

	if _, err := os.Stat(opts.ComposeFile); err != nil {
		return fmt.Errorf("compose file %s not found; run setup from the repository root or pass -compose", opts.ComposeFile)
	}

	step("Checking Docker")
	if err := EnsureDocker(ctx, out); err != nil {
		return err
	}

	if opts.MediaDir != "" || opts.WithIndexers {
		step("Saving configuration")
	}
	if opts.MediaDir != "" {
		abs, err := filepath.Abs(opts.MediaDir)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(abs, 0o755); err != nil {
			return fmt.Errorf("cannot create media folder: %w", err)
		}
		if err := SetEnvValue(envFile, "MARQUEE_MEDIA_DIR", filepath.ToSlash(abs)); err != nil {
			return err
		}
		fmt.Fprintf(out, "Media folder: %s (saved to %s)\n", abs, envFile)
	}
	if opts.WithIndexers {
		// Docker Compose reads COMPOSE_PROFILES from .env, so later up/down runs include Prowlarr too.
		if err := SetEnvValue(envFile, "COMPOSE_PROFILES", "indexers"); err != nil {
			return err
		}
		fmt.Fprintf(out, "Indexer manager (Prowlarr) enabled (saved to %s)\n", envFile)
	}

	step("Building container images")
	build := []string{"build", "--pull"}
	if opts.NoCache {
		build = append(build, "--no-cache")
	}
	if err := Compose(ctx, opts.ComposeFile, build...); err != nil {
		return fmt.Errorf("image build failed: %w", err)
	}

	step("Starting services")
	if err := Compose(ctx, opts.ComposeFile, "up", "-d", "--wait"); err != nil {
		return fmt.Errorf("services did not start: %w", err)
	}

	step("Verifying the installation")
	results := Doctor(ctx, opts.CoreURL)
	Print(out, results)
	if Failed(results) {
		return errors.New("one or more checks failed; see the output above")
	}

	fmt.Fprintf(out, "\nMarquee is running. Core API: %s\n", opts.CoreURL)
	if opts.WithIndexers {
		fmt.Fprintln(out, "Prowlarr: http://127.0.0.1:9696 (add your own indexers there)")
	}
	fmt.Fprintln(out, "Stop it with `marquee down`; start it again with `marquee up`.")
	return nil
}

// SetEnvValue sets key=value in a dotenv file, replacing an existing entry for
// the key or appending one. The file is created if it does not exist.
func SetEnvValue(path, key, value string) error {
	var lines []string
	if f, err := os.Open(path); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			lines = append(lines, sc.Text())
		}
		f.Close()
		if err := sc.Err(); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	entry := key + "=" + value
	replaced := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), key+"=") {
			lines[i] = entry
			replaced = true
		}
	}
	if !replaced {
		lines = append(lines, entry)
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}
