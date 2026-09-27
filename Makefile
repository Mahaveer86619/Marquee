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
	$(LAUNCH) up

down:
	$(LAUNCH) down

doctor:
	$(LAUNCH) doctor

clean:
	go clean
	$(RMBIN)
