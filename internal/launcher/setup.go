package launcher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"marquee/internal/config"
)

// SetupOptions configures a first-time or repeated setup run.
type SetupOptions struct {
	ComposeFile  string
	CoreURL      string
	LibraryDir   string // optional; overrides and saves the library folder
	WithIndexers bool   // enable Prowlarr (compose profile "indexers"); saved
	NoCache      bool   // rebuild images without the build cache
	Out          io.Writer
}

// Setup prepares the data folder and settings, builds the images, starts the
// stack and verifies it. It is safe to run repeatedly. It never writes the
// secrets file (deploy/.env).
func Setup(ctx context.Context, opts SetupOptions) error {
	out := opts.Out
	const total = 5
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

	step("Preparing the data folder")
	home, cfg, err := PrepareHome(opts.LibraryDir, opts.WithIndexers)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Data folder:  %s\nSettings:     %s\nLibrary:      %s\n", home, config.Path(home), cfg.LibraryDir)
	fmt.Fprintf(out, "Secrets file: %s (%s)\n", SecretsFile(opts.ComposeFile), secretsState(opts.ComposeFile))
	env := ComposeEnv(home, cfg)

	step("Building container images")
	build := []string{"build", "--pull"}
	if opts.NoCache {
		build = append(build, "--no-cache")
	}
	if err := Compose(ctx, opts.ComposeFile, env, build...); err != nil {
		return fmt.Errorf("image build failed: %w", err)
	}

	step("Starting services")
	if err := Compose(ctx, opts.ComposeFile, env, "up", "-d", "--wait"); err != nil {
		return fmt.Errorf("services did not start: %w", err)
	}

	step("Verifying the installation")
	results := Doctor(ctx, opts.CoreURL, opts.ComposeFile)
	Print(out, results)
	if Failed(results) {
		return errors.New("one or more checks failed; see the output above")
	}

	fmt.Fprintf(out, "\nMarquee is running. Core API: %s\n", opts.CoreURL)
	if cfg.Indexers.Prowlarr.Enabled {
		fmt.Fprintln(out, "Prowlarr: http://127.0.0.1:9696 (add your own indexers there)")
	}
	fmt.Fprintln(out, "Stop it with `marquee down`; start it again with `marquee up`.")
	return nil
}

// PrepareHome loads (or creates) the settings file, applies setup overrides,
// creates the folders and saves the result.
func PrepareHome(libraryDir string, withIndexers bool) (string, config.Config, error) {
	home, err := config.HomeDir()
	if err != nil {
		return "", config.Config{}, err
	}
	cfg, _, err := config.Load(home)
	if err != nil {
		return "", config.Config{}, err
	}
	if libraryDir != "" {
		abs, err := filepath.Abs(libraryDir)
		if err != nil {
			return "", config.Config{}, err
		}
		cfg.LibraryDir = abs
	}
	if withIndexers {
		cfg.Indexers.Prowlarr.Enabled = true
	}
	if err := config.EnsureLayout(home, cfg); err != nil {
		return "", config.Config{}, err
	}
	if err := config.Save(home, cfg); err != nil {
		return "", config.Config{}, err
	}
	return home, cfg, nil
}

// SecretsFile is the path of the user-managed secrets file next to the compose file.
func SecretsFile(composeFile string) string {
	return filepath.Join(filepath.Dir(composeFile), ".env")
}

// secretsState reports only whether the secrets file exists; it never reads it.
func secretsState(composeFile string) string {
	if _, err := os.Stat(SecretsFile(composeFile)); err == nil {
		return "present"
	}
	return "not created yet; copy deploy/.env.example to deploy/.env to add API keys"
}
