package launcher_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"marquee/internal/config"
	"marquee/internal/launcher"
)

func TestPrepareHomeCreatesSettingsAndLibrary(t *testing.T) {
	home := filepath.Join(t.TempDir(), "marquee-data")
	t.Setenv("MARQUEE_HOME", home)

	gotHome, cfg, err := launcher.PrepareHome("", false)
	if err != nil {
		t.Fatal(err)
	}
	if gotHome != home {
		t.Fatalf("home = %s, want %s", gotHome, home)
	}
	if _, err := os.Stat(config.Path(home)); err != nil {
		t.Fatalf("config.json not written: %v", err)
	}
	if _, err := os.Stat(cfg.LibraryDir); err != nil {
		t.Fatalf("library not created: %v", err)
	}
}

func TestPrepareHomeAppliesAndKeepsOverrides(t *testing.T) {
	home := filepath.Join(t.TempDir(), "marquee-data")
	library := filepath.Join(t.TempDir(), "films")
	t.Setenv("MARQUEE_HOME", home)

	if _, _, err := launcher.PrepareHome(library, true); err != nil {
		t.Fatal(err)
	}
	// A later run without flags keeps the saved choices.
	_, cfg, err := launcher.PrepareHome("", false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LibraryDir != library || !cfg.Indexers.Prowlarr.Enabled {
		t.Fatalf("overrides not persisted: %+v", cfg)
	}
}

func TestComposeEnv(t *testing.T) {
	home := t.TempDir()
	cfg := config.Default(home)

	env := launcher.ComposeEnv(home, cfg)
	if !slices.Contains(env, "MARQUEE_HOME="+filepath.ToSlash(home)) {
		t.Fatal("MARQUEE_HOME missing")
	}
	if !slices.Contains(env, "MARQUEE_LIBRARY_DIR="+filepath.ToSlash(cfg.LibraryDir)) {
		t.Fatal("MARQUEE_LIBRARY_DIR missing")
	}
	if slices.Contains(env, "COMPOSE_PROFILES=indexers") {
		t.Fatal("indexers profile enabled by default")
	}

	cfg.Indexers.Prowlarr.Enabled = true
	if !slices.Contains(launcher.ComposeEnv(home, cfg), "COMPOSE_PROFILES=indexers") {
		t.Fatal("indexers profile missing when Prowlarr is enabled")
	}
}

func TestSetupRequiresComposeFile(t *testing.T) {
	err := launcher.Setup(t.Context(), launcher.SetupOptions{
		ComposeFile: filepath.Join(t.TempDir(), "missing.yaml"),
		Out:         os.Stdout,
	})
	if err == nil {
		t.Fatal("expected an error for a missing compose file")
	}
}

func TestSecretsFileIsNextToComposeFile(t *testing.T) {
	got := launcher.SecretsFile(filepath.Join("deploy", "compose.yaml"))
	if got != filepath.Join("deploy", ".env") {
		t.Fatalf("secrets file = %s", got)
	}
}
