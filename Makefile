ifeq ($(OS),Windows_NT)
EXE := .exe
else
EXE :=
endif

COMPOSE := deploy/compose.yaml
VERSION ?= 0.0.0-dev
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -s -w -X marquee/internal/version.Version=$(VERSION) -X marquee/internal/version.Commit=$(COMMIT)

.PHONY: build test vet check up down doctor clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/marquee$(EXE) ./cmd/marquee
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/marquee-agent$(EXE) ./cmd/agent

test:
	go test ./...

vet:
	go vet ./...

check: vet test
	python -m compileall -q py
	docker compose -f $(COMPOSE) config -q

up: build
	./bin/marquee$(EXE) up

down:
	./bin/marquee$(EXE) down

doctor:
	./bin/marquee$(EXE) doctor

clean:
	go clean
	rm -rf bin
