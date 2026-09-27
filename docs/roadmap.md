# Roadmap

Milestones are delivered in the order below. Each one ends with working, tested software. Items marked optional are not scheduled.

## Build order

### Phase 0: Foundation (complete)

- Go module with core service, launcher and host agent
- Python service skeletons for audio processing and recommendations
- Docker Compose stack with health checks, bound to the loopback interface
- Launcher commands: `up`, `down`, `status`, `logs`, `doctor`
- Draft service contracts

### Phase 1: Streaming proof (M0)

The riskiest part of the system, so it is built first.

- Torrent engine in core with streaming-oriented piece priority
- HTTP range server that waits for pieces instead of failing
- Host agent starts mpv over IPC and plays from the stream URL
- Seeking during download

Exit criterion: a torrent plays in mpv on Windows while it is downloading, with the core running in Docker, and seeking works.

### Phase 2: Library and interface (M1, M1a)

- Metadata search and title details from TMDB
- Release search through user-configured indexers, with compatibility-aware scoring
- Library layout, `core.db` with migrations
- Terminal interface: search, title details, downloads, playback
- Hardware profiler on host and in containers; `marquee doctor` reports the chosen configuration

### Phase 3: Subtitles (M3, M5)

- Embedded subtitle extraction and an external subtitle provider
- Stage A synchronization with live reload in the player
- Stage B synchronization with speech-to-text anchors and piecewise correction
- Synchronization cache

### Phase 4: Viewing experience (M4, M6, M7)

- Continue watching, resume points and next-episode handling
- "Previously on" summary cards
- Recommendations with explanations and a feedback loop
- Watch history import
- Automatic download of upcoming episodes for followed series, within a disk budget

### Phase 5: Storage and audio (M5a, M7a, M7b, M9a)

- Dialogue enhancement presets in the player
- Storage tiers, lossless slimming, trash and approval flow, pinned titles
- Idle-time re-encoding with a quality check; multiple drives
- Dialogue, music and effects separation with independent levels

### Phase 6: Web player and backup (M8, M9)

- Browser player with direct play and remux fallback
- Backup and restore to cloud storage or external drives

### Phase 7: Marquee Link (M12, M12a, M12b)

- Profile identities, QR pairing with verification code, sharing scopes
- Peer-to-peer connectivity with NAT traversal and relay fallback; optional embedded Tailscale
- Friend activity feed and shared recommendation signals
- Co-watching with synchronized playback and live chat
- Voice and video chat in rooms

### Later and optional

- M2: audio output device detection and selection
- M10: collaborative filtering at scale, local summaries, external plugins
- M11: server mode (not planned; the local-first design does not require it)

## Early technical investigations

| Investigation | Question |
|---|---|
| Streaming | Does range streaming from a partially downloaded torrent support reliable seeking in mpv? |
| Container performance | What is torrent write throughput on Windows with a bind mount compared with a named volume? |
| CPU baselines | What speech-to-text and separation throughput can a CPU-only machine achieve inside Docker? |
| Peer-to-peer reachability | How often do direct, hole-punched and relayed connections succeed from Docker on Windows? |
| Co-watch precision | Can two different releases stay within 150 ms of each other using the reference timeline? |
