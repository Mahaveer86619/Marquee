#!/usr/bin/env sh
# Builds the Marquee executables and container images, then starts and
# verifies the stack. Use it after cloning, or any time during development.
#
# Usage: scripts/full-up.sh [--media DIR] [--no-cache]
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"

media=""
nocache=""
while [ $# -gt 0 ]; do
  case "$1" in
    --media) media="$2"; shift 2 ;;
    --no-cache) nocache="-no-cache"; shift ;;
    -h|--help) sed -n '2,6p' "$0"; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 2 ;;
  esac
done

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing prerequisite: $1. $2" >&2
    exit 1
  fi
}

echo "Checking prerequisites"
require go "Install Go 1.26 or later: https://go.dev/dl/"
require docker "Install Docker: https://docs.docker.com/get-docker/"

version="${MARQUEE_VERSION:-0.0.0-dev}"
commit=$(git rev-parse --short HEAD 2>/dev/null || echo unknown)
ldflags="-s -w -X marquee/internal/version.Version=$version -X marquee/internal/version.Commit=$commit"

echo "Building executables ($version, $commit)"
go build -trimpath -ldflags "$ldflags" -o bin/marquee ./cmd/marquee
go build -trimpath -ldflags "$ldflags" -o bin/marquee-agent ./cmd/agent

set -- setup
[ -n "$media" ] && set -- "$@" -media "$media"
[ -n "$nocache" ] && set -- "$@" "$nocache"
exec ./bin/marquee "$@"
