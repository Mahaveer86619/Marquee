package launcher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// dockerStartTimeout bounds how long EnsureDocker waits for Docker Desktop to start.
const dockerStartTimeout = 3 * time.Minute

// EnsureDocker verifies that the Docker CLI is installed and the engine is
// running. On Windows and macOS it starts Docker Desktop if it is installed
// but not running, then waits for the engine to become ready.
func EnsureDocker(ctx context.Context, out io.Writer) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return errors.New("docker was not found on PATH; install Docker Desktop (Windows: winget install Docker.DockerDesktop) and retry")
	}
	if dockerReady(ctx) {
		return nil
	}

	start, ok := desktopStarter()
	if !ok {
		return errors.New("the Docker engine is not running; start Docker and retry")
	}
	fmt.Fprintln(out, "Docker is not running. Starting Docker Desktop...")
	if err := start.Start(); err != nil {
		return fmt.Errorf("could not start Docker Desktop: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, dockerStartTimeout)
	defer cancel()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return errors.New("Docker Desktop did not become ready within 3 minutes; open it manually and retry")
		case <-ticker.C:
			if dockerReady(ctx) {
				fmt.Fprintln(out, "Docker is ready.")
				return nil
			}
		}
	}
}

func dockerReady(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}").Run() == nil
}

// desktopStarter returns a command that launches Docker Desktop, if it is
// installed in its default location on this platform.
func desktopStarter() (*exec.Cmd, bool) {
	switch runtime.GOOS {
	case "windows":
		exe := filepath.Join(os.Getenv("ProgramFiles"), "Docker", "Docker", "Docker Desktop.exe")
		if _, err := os.Stat(exe); err == nil {
			return exec.Command(exe), true
		}
	case "darwin":
		if _, err := os.Stat("/Applications/Docker.app"); err == nil {
			return exec.Command("open", "-a", "Docker"), true
		}
	}
	return nil, false
}
