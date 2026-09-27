// Package launcher implements the host-side launcher: environment checks and
// Docker Compose lifecycle.
package launcher

import (
	"context"
	"fmt"
	"io"
	"net/http"
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
func Doctor(ctx context.Context, coreURL string) []Result {
	return []Result{
		checkBinary("docker", "docker", true),
		checkCommand(ctx, "docker engine", Fail, "docker", "info", "--format", "{{.ServerVersion}}"),
		checkCommand(ctx, "docker compose", Fail, "docker", "compose", "version", "--short"),
		checkBinary("mpv", "mpv", false),
		checkCore(ctx, coreURL),
	}
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
		fmt.Fprintf(w, "[%-4s] %-15s %s\n", r.Status, r.Name, r.Detail)
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

func checkCore(ctx context.Context, coreURL string) Result {
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
