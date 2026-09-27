// Package version holds build metadata injected at link time.
package version

// Overridden with -ldflags "-X marquee/internal/version.Version=... -X marquee/internal/version.Commit=...".
var (
	Version = "0.0.0-dev"
	Commit  = "unknown"
)

// String returns a human-readable version string.
func String() string {
	return Version + " (" + Commit + ")"
}
