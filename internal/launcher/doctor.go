// Package launcher implements the host-side launcher: environment checks and
// Docker Compose lifecycle.
package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Status is the outcome of a single check.
type Status string

const (
	OK   Status = "ok"
	Warn Status = "warn"
	Fail Status = "fail"
)

// Result describes one environment check.
type Result struct {
	Name   string
	Status Status
	Detail string
}

// Doctor runs every environment check in order.
func Doctor(ctx context.Context, coreURL, composeFile string) []Result {
	results := []Result{
		checkBinary("docker", "docker", true),
		checkCommand(ctx, "docker engine", Fail, "docker", "info", "--format", "{{.ServerVersion}}"),
		checkCommand(ctx, "docker compose", Fail, "docker", "compose", "version", "--short"),
		checkBinary("mpv", "mpv", false),
		checkSecretsFile(composeFile),
	}
	core := CheckCore(ctx, coreURL)
	results = append(results, core)
	if core.Status == OK {
		results = append(results, CheckProviders(ctx, coreURL)...)
	}
	return results
}

// providerHints explains what each secret enables, shown when it is not set.
var providerHints = map[string]string{
	"tmdb":          "film search and posters (series search still works through TVmaze)",
	"prowlarr":      "only needed when Prowlarr is enabled",
	"opensubtitles": "online subtitle search (optional)",
	"subdl":         "online subtitle search (optional)",
}

// CheckProviders asks the core which API keys it received. The core reports
// only whether each key is set, never its value.
func CheckProviders(ctx context.Context, coreURL string) []Result {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, coreURL+"/api/v1/status", nil)
	if err != nil {
		return []Result{{"api keys", Fail, err.Error()}}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return []Result{{"api keys", Warn, "could not query the core: " + err.Error()}}
	}
	defer resp.Body.Close()
	var body struct {
		Providers map[string]bool   `json:"providers"`
		Checks    map[string]string `json:"checks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return []Result{{"api keys", Warn, "unexpected status response"}}
	}
	var results []Result
	for _, name := range []string{"tmdb", "prowlarr", "opensubtitles", "subdl"} {
		if !body.Providers[name] {
			results = append(results, Result{name + " key", Warn, "not set: " + providerHints[name]})
			continue
		}
		format := ""
		if f := body.Checks[name+"_format"]; f != "" {
			format = " (" + strings.ReplaceAll(f, "_", " ") + ")"
		}
		switch body.Checks[name] {
		case "valid":
			results = append(results, Result{name + " key", OK, "set" + format + " and accepted by the provider"})
		case "invalid":
			results = append(results, Result{name + " key", Warn, "set" + format + ", but the provider rejected it; check the value in deploy/.env"})
		case "unreachable":
			results = append(results, Result{name + " key", Warn, "set, but the provider could not be reached to verify it"})
		default:
			results = append(results, Result{name + " key", OK, "set"})
		}
	}
	return results
}

// checkSecretsFile reports whether deploy/.env exists without reading it.
func checkSecretsFile(composeFile string) Result {
	path := SecretsFile(composeFile)
	if _, err := os.Stat(path); err != nil {
		return Result{"secrets file", Warn, path + " not found (copy deploy/.env.example to add API keys)"}
	}
	return Result{"secrets file", OK, path}
}

// Failed reports whether any result is a failure.
func Failed(results []Result) bool {
	for _, r := range results {
		if r.Status == Fail {
			return true
		}
	}
	return false
}

// Print writes results as aligned text.
func Print(w io.Writer, results []Result) {
	for _, r := range results {
		fmt.Fprintf(w, "[%-4s] %-18s %s\n", r.Status, r.Name, r.Detail)
	}
}

func checkBinary(name, bin string, required bool) Result {
	path, err := exec.LookPath(bin)
	if err == nil {
		return Result{name, OK, path}
	}
	if required {
		return Result{name, Fail, bin + " not found on PATH"}
	}
	return Result{name, Warn, bin + " not found on PATH (install with: winget install mpv)"}
}

func checkCommand(ctx context.Context, name string, onError Status, bin string, args ...string) Result {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	detail := strings.TrimSpace(string(out))
	if err != nil {
		if detail == "" {
			detail = err.Error()
		}
		return Result{name, onError, firstLine(detail)}
	}
	return Result{name, OK, firstLine(detail)}
}

// CheckCore reports whether the core service answers its health endpoint.
func CheckCore(ctx context.Context, coreURL string) Result {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, coreURL+"/healthz", nil)
	if err != nil {
		return Result{"core service", Fail, err.Error()}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Result{"core service", Warn, "not reachable at " + coreURL + " (run: marquee up)"}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Result{"core service", Fail, fmt.Sprintf("unhealthy: HTTP %d", resp.StatusCode)}
	}
	return Result{"core service", OK, "healthy at " + coreURL}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
