# AGENTS.md — Marquee

Instructions for AI coding agents (Claude Code and others) working in this repo.

## What this project is
Marquee is a local media system with a terminal UI (TUI): it streams torrents while they download, syncs subtitles automatically, tracks what you watch, recommends titles, and manages disk space. The public overview is in `README.md`. The **full design spec** is `notes/PROJECT.md`; read it before any non-trivial change.

## The `notes/` directory (agent memory, never committed)
`notes/` is gitignored. It is the project's working memory across sessions. Treat it as the source of truth for status, decisions, and history.

| File | Purpose | How to update |
|---|---|---|
| `notes/PROJECT.md` | Full idea, detailed design spec | Edit when the design changes; log the change in DECISIONS.md |
| `notes/P2P.md` | Marquee Link transport, pairing and protocols (detailed) | Update when the Link design changes; log decisions in DECISIONS.md |
| `notes/SCHEMA.md` | Database schema + P2P sharing map (authoritative) | Update with every migration; add a changelog line |
| `notes/ROADMAP.md` | Milestone status + achievements log | Tick items, update status, append achievements |
| `notes/DECISIONS.md` | Architecture decision log (ADR-lite) | Append; never rewrite past entries. Supersede instead |
| `notes/FINDINGS.md` | Technical findings, gotchas, benchmarks, library quirks | Append with date + evidence |
| `notes/SESSIONS.md` | One entry per working session | Append at the end of every session |

### Session protocol
**At session start**
1. Read the latest 2–3 entries of `notes/SESSIONS.md`, including their "Next steps".
2. Read `notes/ROADMAP.md` to find the active milestone.
3. Skim `notes/DECISIONS.md` and `notes/FINDINGS.md` for anything that affects the task.

**At session end (required, even for short sessions)**
1. Append a session entry to `notes/SESSIONS.md` using the template in that file.
2. Record any new finding in `FINDINGS.md` and any decision in `DECISIONS.md`.
3. Update milestone status and achievements in `ROADMAP.md`.
4. Use absolute dates (YYYY-MM-DD), never "today" or "last week".

Never copy secrets, API keys, or personal data into notes.

## Hard architecture rules (do not violate without a DECISIONS.md entry)
1. **No video re-encoding in the playback path.** The fallback ladder is direct play → remux (`-c copy`) → audio-only conversion → mpv. Video re-encoding is allowed only in the optimizer's idle-only compression jobs.
2. **Players stream from HTTP URLs** (`/stream/{id}`), never from file paths.
3. **One writing service per SQLite database.** `core.db` is written only by core; `recs.db` only by recs. Nothing else opens them for writing, and no database file is shared between containers.
4. **Docker runs everything except hardware access.** Core, the Python services, ffmpeg and any additional feature services run in containers on every OS, Windows included. Only the launcher/TUI (`marquee.exe`) and the host agent (`marquee-agent.exe`: mpv, audio devices, drives, browser launch, local backups) are native.
5. **Playback has top priority.** Every background job must go through the resource governor and be pausable.
6. **Providers sit behind interfaces** (`MetadataProvider`, `IndexerProvider`, `SubtitleProvider`, `AvailabilityProvider`, `HistoryImporter`, `BackupTarget`). Don't hard-wire a source into core logic.
7. **Deleting user media requires approval** unless the user turned on auto mode. Deleted files go to trash with a grace period first.
8. **CPU-first.** Every feature must work on a machine with no GPU (the developer's machine has none). GPUs and hardware encoders are optional speed-ups chosen by `internal/hwprobe`, based on what is usable **inside the containers**, not just what the host has. Never hard-code a device. Always fall back to CPU when acceleration is missing or fails.
9. **Windows-first, Docker-based.** The first target is Windows with Docker Desktop (WSL2) plus the two native executables. Keep host-side OS code behind build tags (`_windows.go`, `_linux.go`, `_darwin.go`) so Linux and macOS can be added later. A new feature is a new compose service or profile; don't grow the native executables for server-side features.
10. **Don't bundle GPL binaries in the `.exe` release.** ffmpeg lives only in Docker images, installed from Debian packages. mpv is installed on the host by the user or via `winget` after they confirm. The project is MIT-licensed.

11. **Local-first, no hosting.** Marquee runs locally with Docker Compose; the API binds to `127.0.0.1`. Don't build hosted or server features. Per-person tables carry `profile_id`.
12. **SQLite only** (no Postgres). WAL, STRICT tables, UUIDv7 IDs. **`notes/SCHEMA.md` is the schema source of truth:** any migration must update it in the same change.
13. **Marquee Link (P2P) shares metadata only.** Never send media files, file paths, release names, infohashes, magnets, indexer or storage data to peers. Shared payloads use TMDB refs (`tmdb:tv:<id>:sXXeYY`), never local UUIDs. Accept data only from key-pinned, emoji-verified accepted friends, only within granted scopes, and **only directly from the author** (no relaying). Payloads are validated data, never executed. **No hosting:** never add a feature that needs a project-run or user-run server (D-019). Keep identity and trust independent of the transport (libp2p or tsnet).
14. **Security review before shipping Link, co-watch, stream endpoints or secrets handling** (run `/security-review`).

## Stack and conventions
- **Go** (core, agent, tui, optimizer): Go modules at the repo root, `cmd/` for binaries, `internal/` for packages. SQLite via `modernc.org/sqlite` (no cgo), queries through `sqlc`, migrations through `goose`. TUI in Bubble Tea.
- **Python** (`py/audiolab`, `py/recs`): one project per service with `pyproject.toml`, locked with `uv`, built into images. Avoid PyTorch in the default images (prefer ONNX Runtime / CTranslate2); torch-only models go in optional images. gRPC contracts live in `/proto` and are generated for both languages.
- **Web player**: `web/`, TypeScript.
- **Deploy**: `deploy/compose.yaml` (profiles `cpu`, `gpu-nvidia`, `indexers`, `stems`, plus one per additional feature service) is embedded in `marquee.exe`, which runs `docker compose -p marquee`. Host executables are `GOOS=windows` builds. A native core build must keep compiling for dev/debug, but isn't a user mode.
- **License**: MIT (`LICENSE`). New dependencies must be license-compatible: MIT, BSD, Apache-2.0 or MPL-2.0 are fine. Flag any GPL/AGPL dependency in DECISIONS.md before adding it.
- Match the surrounding code's style. Keep comments sparse and useful.

## Scope and legal guardrail
Marquee ships with **no content sources**. Don't add built-in scrapers for torrent sites or streaming platforms, and don't add DRM circumvention. Indexers come from user configuration (Torznab / Prowlarr). Streaming-platform data comes only from official or public APIs (TMDB watch providers, Trakt) and the user's own exports.

## Current phase
Phase 0 (foundation) is done as of 2026-09-27: Go module `marquee` (`cmd/core`, `cmd/marquee`, `cmd/agent`, `internal/{api,launcher,version}`), Python skeletons (`py/audiolab`, `py/recs`), `deploy/compose.yaml` + `deploy/docker/*.Dockerfile`, draft protos, public docs in `docs/`, CI. **Next: M0 streaming proof** (see `docs/roadmap.md` and `notes/ROADMAP.md`).

- `make check` must pass before a change is done (vet, test, Python compile, compose config).
- **Never read, print or edit `deploy/.env`** (user secrets). The user creates it from `deploy/.env.example`. To check keys, use `marquee doctor` or `GET /api/v1/status`, which report presence only. New secrets get an entry in `deploy/.env.example` and a presence flag in `configuredProviders` (cmd/core), never a logged value. Non-secret settings belong in `config.json` (`internal/config`), not `.env`.
- **Database access goes through sqlc.** Write each query as a named query in `internal/store/queries/*.sql`, then run `make generate` (sqlc 1.31.1 in Docker) and commit `internal/store/db/`. Never edit the generated files, and never put SQL strings in Go outside the migration runner. `internal/store` maps generated rows to `catalog` types. Schema changes are new files in `internal/store/migrations/` (never edit an applied one) plus an entry in `notes/SCHEMA.md`. `make generate-check` must pass. Set `MARQUEE_LOG_SQL=1` to log each query's name and timing.
- **Tests live in `tests/`**, never next to the code. Use `tests/<component>/` with `package <component>_test`, importing `marquee/internal/...`. Tests that need the running stack go in `tests/integration/` behind `//go:build integration` (`make test-integration`). Fixtures go in `tests/fixtures/`. To test something unexported, export it deliberately with a doc comment.
- **Docker covers every install.** Users need only Docker. ffmpeg, the Python services, Prowlarr (profile `indexers`) and new tools run as containers. The host executables build in `golang:1.27` when Go is absent (scripts/full-up). Never require a host install beyond Docker; mpv is the only host-side exception, and it's optional.
- On Windows, don't write files with .NET `[IO.File]` methods and relative paths: they resolve against the process directory, not the PowerShell location (see F-018). Use the Edit/Write tools.
- Public docs (`README.md`, `docs/`) are formal, contain no emojis, and describe **only the current product scope (a single release, v1)**. No version splits, no "later" or deferred features, no internal milestone IDs. Items that aren't planned stay in `notes/` only. Keep public docs in sync with `notes/`; `notes/` holds the detail.
- `notes/` also avoids version splits: everything planned is v1. Items the user set aside are marked "not planned" or "optional", never "v2" or "later version".
- The user manages git themselves. Don't commit unless asked.
- Module path is `marquee` for now; switch to `github.com/<owner>/marquee` once the GitHub repo exists.

## Scope
Build only what `notes/ROADMAP.md` schedules. Items marked "not planned" or "optional" there are out of scope unless the user asks for them.
