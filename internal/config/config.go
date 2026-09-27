// Package config manages Marquee's user settings file (config.json) and the
// data folder that holds it. Secrets never go here; they live in deploy/.env.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// FileName is the settings file inside the data folder.
const FileName = "config.json"

// CurrentVersion is the schema version written to new config files.
const CurrentVersion = 1

// Config holds user settings. It is safe to edit by hand.
type Config struct {
	Version    int      `json:"version"`
	LibraryDir string   `json:"library_dir"` // host folder for completed downloads
	Indexers   Indexers `json:"indexers"`
	Metadata   Metadata `json:"metadata"`
	Playback   Playback `json:"playback"`
}

// Indexers configures user-supplied release sources.
type Indexers struct {
	Prowlarr Prowlarr `json:"prowlarr"`
}

// Prowlarr configures the optional Prowlarr container. Its API key is a secret
// and is read from MARQUEE_PROWLARR_API_KEY, not from this file.
type Prowlarr struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"` // as seen from the core container
}

// Metadata configures metadata lookups.
type Metadata struct {
	Language string `json:"language"` // e.g. "en-US"
	Region   string `json:"region"`   // ISO 3166-1, for streaming availability
}

// Playback holds track preferences, in priority order (ISO 639-2 codes).
type Playback struct {
	AudioLanguages    []string `json:"audio_languages"`
	SubtitleLanguages []string `json:"subtitle_languages"`
}

// HomeDir returns the data folder: $MARQUEE_HOME if set, otherwise
// <user home>/marquee-data.
func HomeDir() (string, error) {
	if v := os.Getenv("MARQUEE_HOME"); v != "" {
		return filepath.Abs(v)
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine the user home folder: %w", err)
	}
	return filepath.Join(h, "marquee-data"), nil
}

// Default returns the settings used when no config file exists yet.
func Default(home string) Config {
	return Config{
		Version:    CurrentVersion,
		LibraryDir: filepath.Join(home, "library"),
		Indexers:   Indexers{Prowlarr: Prowlarr{Enabled: false, URL: "http://prowlarr:9696"}},
		Metadata:   Metadata{Language: "en-US", Region: "US"},
		Playback: Playback{
			AudioLanguages:    []string{"eng"},
			SubtitleLanguages: []string{"eng"},
		},
	}
}

// Path returns the config file path inside a data folder.
func Path(home string) string {
	return filepath.Join(home, FileName)
}

// Load reads the config file in home. If the file does not exist, it returns
// the defaults and exists=false. Missing fields keep their default values.
func Load(home string) (cfg Config, exists bool, err error) {
	cfg = Default(home)
	data, err := os.ReadFile(Path(home))
	if errors.Is(err, os.ErrNotExist) {
		return cfg, false, nil
	}
	if err != nil {
		return cfg, false, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, true, fmt.Errorf("%s is not valid JSON: %w", Path(home), err)
	}
	return cfg, true, nil
}

// Save writes the config file atomically.
func Save(home string, cfg Config) error {
	if err := os.MkdirAll(home, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := Path(home) + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, Path(home))
}

// EnsureLayout creates the data folder and the library folder.
func EnsureLayout(home string, cfg Config) error {
	for _, dir := range []string{home, cfg.LibraryDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("cannot create %s: %w", dir, err)
		}
	}
	return nil
}
