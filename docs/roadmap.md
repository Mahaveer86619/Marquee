# Roadmap

Marquee is built in the phases below. Each phase ends with working, tested software.

## Build order

### Phase 0: Foundation (complete)

- Go module with core service, launcher and host agent
- Python service skeletons for audio processing and recommendations
- Docker Compose stack with health checks, bound to the loopback interface
- Launcher commands: `up`, `down`, `status`, `logs`, `doctor`
- Draft service contracts

### Phase 1: First runnable slice (search, download, play in the browser)

The first slice a user can run end to end. Design decisions come from the [field study](field-study.md).

| Step | Deliverable |
|---|---|
| 1. Storage | `core.db` with migrations for titles, items, releases, torrents, files, tracks and the download queue |
| 2. Metadata | TMDB client (key supplied by the user) and a keyless TVmaze fallback. Search, title details, seasons and episodes. Attribution screen. |
| 3. Sources | Source provider interface with three implementations: Internet Archive (built in, public domain), Torznab (Prowlarr, Jackett, bitmagnet), and manual magnet or torrent file |
| 4. Releases | Release-name parsing, file selection inside multi-file torrents, and scoring that favours browser-compatible releases |
| 5. Downloads | Embedded torrent engine behind an interface. Queue, per-file selection, progress events over WebSocket. In-progress data on a named volume, moved to the library on completion. |
| 6. Probing | ffprobe records the container, codecs, audio tracks, subtitle tracks and keyframes of every file |
| 7. Playback | Browser player at `/watch/{id}`: direct play when compatible, otherwise HLS remux with the video copied and one playlist per audio track. Embedded and bundled text subtitles as WebVTT. Plays while downloading. |
| 8. Terminal interface | Search box, results, title detail with seasons and episodes, release picker, downloads view with progress, and an action that opens the browser player |

Exit criteria:
- A public-domain film found through search downloads from the Internet Archive, then plays in the browser while it is still downloading, with seeking.
- A test file with two audio tracks and two subtitle tracks allows switching between them.
- Every file path and track is recorded in `core.db`.

### Phase 2: Player integration and hardware

- Host agent starts mpv over IPC for files the browser cannot play
- Hardware profiler on host and in containers; `marquee doctor` reports the chosen configuration
- Styled (ASS) and image (PGS) subtitles in the browser player

### Phase 3: Subtitles

- Embedded subtitle extraction and an external subtitle provider
- Stage A synchronization with live reload in the player
- Stage B synchronization with speech-to-text anchors and piecewise correction
- Synchronization cache

### Phase 4: Viewing experience

- Continue watching, resume points and next-episode handling
- "Previously on" summary cards
- Recommendations with explanations, a feedback loop and collaborative filtering
- Watch history import
- Automatic download of upcoming episodes for followed series, within a disk budget

### Phase 5: Storage and audio

- Dialogue enhancement presets in the player
- Storage tiers, lossless slimming, trash and approval flow, pinned titles
- Idle-time re-encoding with a quality check; multiple drives
- Dialogue, music and effects separation with independent levels

### Phase 6: Player features, backup and extensibility

- Styled and image subtitles, and further player controls
- Backup and restore to cloud storage or external drives
- External provider plugins

### Phase 7: Marquee Link

- Profile identities, QR pairing with verification code, sharing scopes
- Peer-to-peer connectivity with NAT traversal and relay fallback; optional embedded Tailscale
- Friend activity feed and shared recommendation signals
- Co-watching with synchronized playback and live chat
- Voice and video chat in rooms

## Early technical investigations

| Investigation | Question |
|---|---|
| Streaming | Does range streaming from a partially downloaded torrent support reliable seeking in mpv? |
| Container performance | What is torrent write throughput on Windows with a bind mount compared with a named volume? |
| CPU baselines | What speech-to-text and separation throughput can a CPU-only machine achieve inside Docker? |
| Peer-to-peer reachability | How often do direct, hole-punched and relayed connections succeed from Docker on Windows? |
| Co-watch precision | Can two different releases stay within 150 ms of each other using the reference timeline? |
