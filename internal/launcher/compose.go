package launcher

import (
	"context"
	"os"
	"os/exec"
)

// ProjectName is the Docker Compose project name used for every Marquee stack.
const ProjectName = "marquee"

// Compose runs `docker compose` against the given compose file with output
// streamed to the terminal.
func Compose(ctx context.Context, composeFile string, args ...string) error {
	full := append([]string{"compose", "-p", ProjectName, "-f", composeFile}, args...)
	cmd := exec.CommandContext(ctx, "docker", full...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
