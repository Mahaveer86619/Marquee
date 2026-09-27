ifeq ($(OS),Windows_NT)
EXE     := .exe
NULL    := NUL
LAUNCH  := bin\marquee.exe
RMBIN   := if exist bin rmdir /s /q bin
else
EXE     :=
NULL    := /dev/null
LAUNCH  := ./bin/marquee
RMBIN   := rm -rf bin
endif

COMPOSE := deploy/compose.yaml
VERSION ?= 0.0.0-dev
COMMIT  ?= $(or $(shell git rev-parse --short HEAD 2>$(NULL)),unknown)
LDFLAGS := -s -w -X marquee/internal/version.Version=$(VERSION) -X marquee/internal/version.Commit=$(COMMIT)

SQLC := docker run --rm -v "$(CURDIR):/src" -w /src sqlc/sqlc:1.31.1

.PHONY: build test test-integration vet check generate generate-check full-up up down doctor logs logs-core clean

# Regenerate typed database code from internal/store/queries (runs sqlc in Docker).
generate:
	$(SQLC) generate

# Fails when the generated code is out of date with the queries or migrations.
generate-check:
	$(SQLC) diff

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/marquee$(EXE) ./cmd/marquee
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/marquee-agent$(EXE) ./cmd/agent

test:
	go test ./...

# Requires a running stack (scripts/full-up or `marquee up`).
test-integration:
	go test -tags integration -count=1 ./tests/integration

vet:
	go vet ./...

check: vet test
	python -m compileall -q py
	docker compose -f $(COMPOSE) config -q

# Build executables and images, start the stack and verify it.
# Optional: make full-up MEDIA=D:/Media NOCACHE=1
full-up: build
	$(LAUNCH) setup $(if $(MEDIA),-media $(MEDIA)) $(if $(NOCACHE),-no-cache)

up: build
	$(LAUNCH) up

down:
	$(LAUNCH) down

doctor:
	$(LAUNCH) doctor

# Follow live logs. All services by default: make logs SERVICE=core
logs:
	$(LAUNCH) logs $(SERVICE)

# Shortcut for the core service's logs.
logs-core:
	$(LAUNCH) logs core

clean:
	go clean
	$(RMBIN)
