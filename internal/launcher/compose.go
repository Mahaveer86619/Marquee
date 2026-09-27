package launcher

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"

	"marquee/internal/config"
)

// ProjectName is the Docker Compose project name used for every Marquee stack.
const ProjectName = "marquee"

// ComposeEnv returns the environment for docker compose: the data and library
// folders from the user's settings, and the enabled optional profiles.
func ComposeEnv(home string, cfg config.Config) []string {
	env := append(os.Environ(),
		"MARQUEE_HOME="+filepath.ToSlash(home),
		"MARQUEE_LIBRARY_DIR="+filepath.ToSlash(cfg.LibraryDir),
	)
	if cfg.Indexers.Prowlarr.Enabled {
		env = append(env, "COMPOSE_PROFILES=indexers")
	}
	return env
}

// Compose runs `docker compose` against the given compose file with output
// streamed to the terminal.
func Compose(ctx context.Context, composeFile string, env []string, args ...string) error {
	full := append([]string{"compose", "-p", ProjectName, "-f", composeFile}, args...)
	cmd := exec.CommandContext(ctx, "docker", full...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
