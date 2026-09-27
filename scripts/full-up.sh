#!/usr/bin/env sh
# Builds the Marquee executables and container images, then starts and
# verifies the stack. Docker is the only prerequisite: without Go, the
# executables are built inside a Go container.
#
# Usage: scripts/full-up.sh [--media DIR] [--with-indexers] [--no-cache] [--build-in-docker]
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"

media=""
indexers=""
nocache=""
docker_go=""
while [ $# -gt 0 ]; do
  case "$1" in
    --media) media="$2"; shift 2 ;;
    --with-indexers) indexers="-with-indexers"; shift ;;
    --no-cache) nocache="-no-cache"; shift ;;
    --build-in-docker) docker_go=1; shift ;;
    -h|--help) sed -n '2,7p' "$0"; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 2 ;;
  esac
done

echo "Checking prerequisites"
if ! command -v docker >/dev/null 2>&1; then
  echo "Missing prerequisite: Docker. Install it from https://docs.docker.com/get-docker/" >&2
  exit 1
fi
command -v go >/dev/null 2>&1 || docker_go=1

version="${MARQUEE_VERSION:-0.0.0-dev}"
commit=$(git rev-parse --short HEAD 2>/dev/null || echo unknown)
ldflags="-s -w -X marquee/internal/version.Version=$version -X marquee/internal/version.Commit=$commit"

case "$(uname -s)" in Darwin) goos=darwin ;; *) goos=linux ;; esac
case "$(uname -m)" in aarch64|arm64) goarch=arm64 ;; *) goarch=amd64 ;; esac

build() {
  if [ -n "$docker_go" ]; then
    docker run --rm --user "$(id -u):$(id -g)" -v "$root:/src" -w /src \
      -e CGO_ENABLED=0 -e GOOS="$goos" -e GOARCH="$goarch" \
      -e GOCACHE=/src/.cache/go-build -e GOMODCACHE=/src/.cache/gomod \
      golang:1.27 go build -trimpath -ldflags "$ldflags" -o "$1" "$2"
  else
    go build -trimpath -ldflags "$ldflags" -o "$1" "$2"
  fi
}

if [ -n "$docker_go" ]; then
  if ! docker info >/dev/null 2>&1; then
    echo "Docker is not running. Start it and run this script again." >&2
    exit 1
  fi
  echo "Building executables in a Go container ($version, $commit)"
else
  echo "Building executables ($version, $commit)"
fi
build bin/marquee ./cmd/marquee
build bin/marquee-agent ./cmd/agent

set -- setup
[ -n "$media" ] && set -- "$@" -media "$media"
[ -n "$indexers" ] && set -- "$@" "$indexers"
[ -n "$nocache" ] && set -- "$@" "$nocache"
exec ./bin/marquee "$@"
