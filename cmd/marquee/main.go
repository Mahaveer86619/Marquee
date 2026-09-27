// Command marquee is the host launcher. It checks the environment, manages the
// Docker Compose stack, and will host the terminal UI.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"marquee/internal/config"
	"marquee/internal/launcher"
	"marquee/internal/version"
)

const usage = `Usage: marquee [flags] <command> [command flags]

Commands:
  setup     First-time setup: check Docker, prepare the data folder, build, start and verify
  up        Build (if needed) and start the stack in the background
  down      Stop the stack (volumes are kept)
  status    Show the state of each service
  logs      Follow service logs
  doctor    Check Docker, the core service, API keys and host tools
  config    Show the data folder and settings (config.json)
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
		library := fs.String("media", "", "host folder for completed downloads (default: <data folder>/library; saved)")
		withIndexers := fs.Bool("with-indexers", false, "also run Prowlarr for user-configured indexers (saved)")
		noCache := fs.Bool("no-cache", false, "rebuild images without the build cache")
		_ = fs.Parse(args)
		err = launcher.Setup(ctx, launcher.SetupOptions{
			ComposeFile:  *composeFile,
			CoreURL:      *coreURL,
			LibraryDir:   *library,
			WithIndexers: *withIndexers,
			NoCache:      *noCache,
			Out:          os.Stdout,
		})
	case "up", "down", "status", "logs":
		err = compose(ctx, *composeFile, cmd)
	case "doctor":
		results := launcher.Doctor(ctx, *coreURL, *composeFile)
		launcher.Print(os.Stdout, results)
		if launcher.Failed(results) {
			os.Exit(1)
		}
	case "config":
		err = showConfig()
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

func compose(ctx context.Context, composeFile, cmd string) error {
	home, err := config.HomeDir()
	if err != nil {
		return err
	}
	cfg, _, err := config.Load(home)
	if err != nil {
		return err
	}
	args := map[string][]string{
		"up":     {"up", "-d", "--build", "--wait"},
		"down":   {"down"},
		"status": {"ps"},
		"logs":   {"logs", "-f", "--tail", "100"},
	}[cmd]
	return launcher.Compose(ctx, composeFile, launcher.ComposeEnv(home, cfg), args...)
}

func showConfig() error {
	home, err := config.HomeDir()
	if err != nil {
		return err
	}
	cfg, exists, err := config.Load(home)
	if err != nil {
		return err
	}
	fmt.Println("Data folder:", home)
	if exists {
		fmt.Println("Settings:   ", config.Path(home))
	} else {
		fmt.Println("Settings:   ", config.Path(home), "(not created yet; run `marquee setup`)")
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	fmt.Println(string(data))
	return nil
}
