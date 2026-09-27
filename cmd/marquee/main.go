// Command marquee is the host launcher. It checks the environment, manages the
// Docker Compose stack, and will host the terminal UI.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"marquee/internal/launcher"
	"marquee/internal/version"
)

const usage = `Usage: marquee [flags] <command> [command flags]

Commands:
  setup     First-time setup: check Docker, build images, start and verify the stack
  up        Build (if needed) and start the stack in the background
  down      Stop the stack (volumes are kept)
  status    Show the state of each service
  logs      Follow service logs
  doctor    Check Docker, the core service and host tools
  version   Print the version

Run "marquee setup -h" for setup options.

Flags:
`

func main() {
	composeFile := flag.String("compose", "deploy/compose.yaml", "path to the compose file")
	coreURL := flag.String("core", "http://127.0.0.1:7700", "core service URL")
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cmd, args := flag.Arg(0), flag.Args()[1:]
	if cmd != "setup" && len(args) > 0 {
		fmt.Fprintf(os.Stderr, "%s takes no arguments\n", cmd)
		os.Exit(2)
	}

	var err error
	switch cmd {
	case "setup":
		fs := flag.NewFlagSet("setup", flag.ExitOnError)
		media := fs.String("media", "", "host folder for the media library (saved for later runs)")
		withIndexers := fs.Bool("with-indexers", false, "also run Prowlarr for user-configured indexers (saved for later runs)")
		noCache := fs.Bool("no-cache", false, "rebuild images without the build cache")
		_ = fs.Parse(args)
		err = launcher.Setup(ctx, launcher.SetupOptions{
			ComposeFile:  *composeFile,
			CoreURL:      *coreURL,
			MediaDir:     *media,
			WithIndexers: *withIndexers,
			NoCache:      *noCache,
			Out:          os.Stdout,
		})
	case "up":
		err = launcher.Compose(ctx, *composeFile, "up", "-d", "--build", "--wait")
	case "down":
		err = launcher.Compose(ctx, *composeFile, "down")
	case "status":
		err = launcher.Compose(ctx, *composeFile, "ps")
	case "logs":
		err = launcher.Compose(ctx, *composeFile, "logs", "-f", "--tail", "100")
	case "doctor":
		results := launcher.Doctor(ctx, *coreURL)
		launcher.Print(os.Stdout, results)
		if launcher.Failed(results) {
			os.Exit(1)
		}
	case "version":
		fmt.Println("marquee", version.String())
	default:
		flag.Usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
