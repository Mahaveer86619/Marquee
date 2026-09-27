# Architecture

This document describes how Marquee is structured and why. It reflects the current design. Parts of it are not implemented yet; see the [roadmap](roadmap.md).

## Goals

- Start playback within seconds of choosing a title, while the file is still downloading.
- Never re-encode video in the playback path.
- Run completely on the user's machine, with no hosted backend.
- Work on machines without a GPU, and use acceleration automatically when it is available.
- Keep friends' installations connected directly, without a server in between.

## Runtime model

Marquee runs as a Docker Compose stack on every platform, including Windows (Docker Desktop with WSL2). Two native programs complement it.

| Component | Where | Responsibility |
|---|---|---|
| `marquee` | Host | Launcher and terminal UI. Checks the environment, starts the stack, and shows the interface. |
| `marquee-agent` | Host | Controls mpv over IPC, opens the browser player, detects removable drives, runs local backups, and reports host hardware. |
| `core` | Container | API, scheduler, torrent engine, stream server, metadata, library, storage manager, resource governor. Owns `core.db`. |
| `audiolab` | Container | Voice activity detection, speech-to-text subtitle alignment, dialogue stem separation. |
| `recs` | Container | Embeddings, taste profiles, collaborative filtering, friend signals. Owns `recs.db`. |
| `optimizer` | Container (planned) | Lossless slimming and idle-time re-encoding. |
| `social` | Container (planned) | Marquee Link: identities, friends, feeds, co-watch rooms, chat. Owns `social.db`. |

Hardware-facing work stays on the host because Docker Desktop runs containers inside a virtual machine, which has no access to the display, the audio devices or hot-plugged drives.

### Communication

- **Clients to core:** REST for commands and one WebSocket event stream for progress and state changes.
- **Core to workers:** gRPC. The contracts are in [`proto/`](../proto).
- **Player to core:** HTTP range requests on `/stream/{id}`. The player never opens library files directly, so the same flow works for complete and partially downloaded files.

## Playback path

1. The torrent engine downloads the first and last pieces of the file first. Container formats keep their index at those positions, and the player needs them to start and seek.
2. Pieces are then prioritized in a window ahead of the playback position. Seeking moves the window.
3. The stream server answers range requests. When a requested piece is not yet available, the request waits for it instead of failing.
4. mpv plays the stream directly. The browser player uses the same stream when the codecs are browser-compatible, remuxes without re-encoding when only the container is the problem, and converts only the audio track when the audio codec is unsupported.

Release selection takes player compatibility into account, so most titles need no processing at all.

## Subtitle synchronization

Synchronization runs in two stages, on audio decoded only from pieces that have already been downloaded.

- **Stage A** builds a speech activity timeline and cross-correlates it with the subtitle timing. This corrects constant offsets and frame-rate differences within seconds.
- **Stage B** transcribes short windows with a speech-to-text model, matches the recognized words against the subtitle text, and fits a piecewise-linear correction. This handles different cuts and removed segments.

Corrections are applied to the running player and cached per file and subtitle. The same correction also maps each file onto a shared reference timeline, which co-watching uses to keep different releases in sync.

## Resource governor

All background work runs through a priority scheduler in core:

| Priority | Work |
|---|---|
| 1 | Stream serving and pieces near the playback position |
| 2 | Subtitle alignment and stem separation near the playback position |
| 3 | On-demand downloads |
| 4 | Prefetch downloads and work for upcoming episodes |
| 5 | Lossless slimming |
| 6 | Idle-time re-encoding |
| 7 | Model training and backups |

When playback starts buffering, work at priority 4 and below is paused.

## Hardware profiling

A profiler runs on the host and inside each container, because what the machine has and what the containers can use differ. It measures CPU and memory limits, detects GPUs and hardware encoders (confirmed by a test encode), and runs short benchmarks. The results select model sizes and encoders. The CPU-only configuration is the reference path and is always available.

## Data ownership

Each database has exactly one service that writes to it:

| Database | Writer |
|---|---|
| `core.db` | core |
| `recs.db` | recs |
| `social.db` | social |

Services exchange data through events over gRPC and never query each other's databases. See [data-model.md](data-model.md).

## Security model

- The core API listens on `127.0.0.1` only.
- Marquee Link sessions are encrypted and mutually authenticated with keys pinned during pairing.
- Peer payloads are validated as data and are accepted only from verified friends within the scopes they were granted.
- Secrets such as API keys are encrypted at rest.

## Platform order

1. Windows, with Docker Desktop and native launcher and agent
2. Linux, with Docker Engine
3. macOS, with Docker Desktop
