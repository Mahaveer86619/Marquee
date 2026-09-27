package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"marquee/internal/config"
)

func TestHomeDirDefaultsToUserHome(t *testing.T) {
	t.Setenv("MARQUEE_HOME", "")
	home, err := config.HomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(home) != "marquee-data" {
		t.Fatalf("home = %s, want .../marquee-data", home)
	}
}

func TestHomeDirOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MARQUEE_HOME", dir)
	home, err := config.HomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if home != dir {
		t.Fatalf("home = %s, want %s", home, dir)
	}
}

func TestLoadMissingReturnsDefaults(t *testing.T) {
	home := t.TempDir()
	cfg, exists, err := config.Load(home)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("exists = true for a missing file")
	}
	if cfg.LibraryDir != filepath.Join(home, "library") || cfg.Indexers.Prowlarr.Enabled {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	home := t.TempDir()
	cfg := config.Default(home)
	cfg.Indexers.Prowlarr.Enabled = true
	cfg.Metadata.Region = "IN"
	if err := config.Save(home, cfg); err != nil {
		t.Fatal(err)
	}
	got, exists, err := config.Load(home)
	if err != nil || !exists {
		t.Fatalf("load: exists=%v err=%v", exists, err)
	}
	if !got.Indexers.Prowlarr.Enabled || got.Metadata.Region != "IN" {
		t.Fatalf("round trip lost values: %+v", got)
	}
}

func TestLoadKeepsDefaultsForMissingFields(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(config.Path(home), []byte(`{"metadata":{"region":"IN"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Metadata.Region != "IN" || cfg.Metadata.Language != "en-US" || len(cfg.Playback.AudioLanguages) == 0 {
		t.Fatalf("defaults not preserved: %+v", cfg)
	}
}

func TestLoadRejectsInvalidJSON(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(config.Path(home), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := config.Load(home); err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
}

func TestEnsureLayoutCreatesFolders(t *testing.T) {
	home := filepath.Join(t.TempDir(), "marquee-data")
	cfg := config.Default(home)
	if err := config.EnsureLayout(home, cfg); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(cfg.LibraryDir); err != nil || !fi.IsDir() {
		t.Fatalf("library folder not created: %v", err)
	}
}
