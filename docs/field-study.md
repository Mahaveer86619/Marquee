# Field study

This study was carried out on 2026-09-27, before the architecture of the first runnable version was finalized. It compares the available options for each part of the search, download and play path, and records the resulting decisions. Items that could not be verified against a current source are marked "unverified" and must be checked during implementation.

## 1. Similar projects

| Project | Language and license | Approach | Lesson for Marquee |
|---|---|---|---|
| Stremio | Rust core (MIT), web client (GPL-2.0); the streaming server is closed source | Add-ons provide catalogs and streams; a local server fetches pieces and serves HTTP | The add-on model for sources is sound. An open streaming server is a gap. |
| Seanime | Go and TypeScript, GPL-3.0 | Torrent streaming with anacrolix/torrent, web player, SQLite | The closest architecture. GPL, so ideas only; no code reuse. |
| Jellyfin | C#, GPL-2.0 | Library server; direct play, remux or transcode per device profile | Probe first, then choose the cheapest playback path. Keep the time to first frame short. |
| Sonarr, Radarr, Prowlarr | C#, GPL-3.0 | Torznab indexers, release parsing, quality profiles, external download client | Torznab as the indexer interface; quality scoring for the release picker. |
| Overseerr / Jellyseerr (now Seerr) | TypeScript | Request interface on top of TMDB | A good model for search and title detail screens. |
| peerflix, webtorrent-cli | JavaScript, MIT | Magnet to local HTTP server to external player | The minimal loop: piece priority plus a range server. |
| webtor.io | Go, MIT | anacrolix/torrent with on-demand ffmpeg HLS segments | The most relevant permissively licensed reference for "play while downloading". |
| ani-cli, lobster, mov-cli | Shell and Python | Scraped sites, a fuzzy picker, mpv | Keyboard flow is fast; scrapers are fragile. |

No existing tool combines all of the following: a fully open, permissively licensed streaming server; films and television; a terminal-first interface with a browser player; and a library that records the tracks inside each file.

## 2. Torrent engine

| Option | License | Streaming with seek | Per-file selection | Integration from Go |
|---|---|---|---|---|
| anacrolix/torrent | MPL-2.0 | Yes: file readers raise priority around the read position and plug directly into `http.ServeContent` | Yes | Native library |
| librqbit (rqbit) | Apache-2.0 | Yes: built-in HTTP range stream | Yes | REST sidecar |
| libtorrent-rasterbar | BSD-3-Clause | Yes, with piece deadlines; streaming API must be built | Yes | Sidecar with a custom API |
| qBittorrent Web API | GPL (used over HTTP) | Sequential and first/last piece only; no seek-aware priority | Yes | REST, poor seeking |
| Transmission RPC | GPL (used over RPC) | Sequential from a given piece | Yes | RPC, crude seeking |
| cenkalti/rain | MIT | Sequential only | No | Unsuitable |

Seanime, webtor.io and TorrServer all stream with anacrolix/torrent.

Known anacrolix issues:
- Memory is not released after a torrent is dropped (issue 930).
- SQLite storage is slow (issue 890).
- Files can stay locked after removal (issue 1062).
- A data race between reading and changing readahead was reported on 2026-09-25 (issue 1118).

**Decision:** embed anacrolix/torrent behind a `TorrentEngine` interface.
- Use file storage on a Docker named volume.
- Run a single client and cap the number of active torrents.
- Change readahead only from the goroutine that reads.
- If memory, CPU or file locking prove to be a problem, librqbit is the replacement candidate.

## 3. Metadata

| Source | Access | Terms | Role |
|---|---|---|---|
| TMDB | Free API key (v4 read token) | Attribution with logo and notice; cache no longer than six months; JustWatch credit wherever watch-provider data is shown | Primary for films and series |
| TVmaze | No key | Data under CC BY-SA 4.0, attribution required | Fallback for series; works without configuration |
| TheTVDB | Paid, or free under a revenue threshold | Attribution | Optional |
| Trakt | New API applications now require a paid account (since July 2026) | | Dropped from the plan |
| OMDb, Wikidata, AniList | Various | | Supplementary: ratings, ID mapping and anime |

**Decision:**
- TMDB is the primary source, with a key supplied by the user.
- TVmaze is the keyless fallback for series.
- An attribution screen is required.

## 4. Release sources

- **Built-in legal source: Internet Archive.**
  - Search through `advancedsearch.php`, restricted to feature films.
  - List files through `/metadata/{id}/files`.
  - Each item has a torrent at `/download/{id}/{id}_archive.torrent`, and these torrents use web seeds, so they download with no peers.
  - Results are filtered by `licenseurl` so that only public-domain and openly licensed items are shown.
  - Television coverage is minimal.
- **Test fixtures:** the Blender open movies distributed by WebTorrent (for example Big Buck Bunny and Sintel) are openly licensed and web-seeded.
- **User-configured indexers:** one generic Torznab client covers Prowlarr, Jackett and bitmagnet.
  - Capabilities come from `t=caps`.
  - Searches use `t=search`, `t=tvsearch` (season and episode) or `t=movie`.
  - Results come from `torznab:attr` fields: seeders, size, infohash and magnet link.
- **Manual input:** a magnet link or `.torrent` file.

Marquee ships no indexer definitions.

## 5. Release-name parsing and file selection

- Sonarr's parser is GPL and guessit is LGPL. Both are studied for behavior only, and no code is copied.
- **jhin** (Go, MIT) is a successor to parse-torrent-title and is tested against a corpus of more than a thousand titles. It is very new, so it is pinned or vendored, behind an interface.
- **Selecting files inside a torrent:**
  1. Parse every file path.
  2. Keep video files and discard samples and very small files.
  3. Match on season and episode (or absolute number).
  4. Fall back to the largest file.
  5. Pair external subtitles by file stem and language code.
- Audio tracks are read with ffprobe, because release names are unreliable.

## 6. Subtitles

- **Primary:** embedded tracks and subtitle files included in the torrent.
- **Additional:** online providers enabled by the user.
  - OpenSubtitles requires an API key. Its daily download quotas are small: 5 per IP anonymously and 20 with a free account.
  - SubDL issues a key per user with 2,000 requests per day.
- Addic7ed has no API and is excluded.

## 7. Browser playback

| | Chrome / Edge | Firefox | Safari |
|---|---|---|---|
| MP4 / fragmented MP4 | Yes | Yes | Yes |
| MKV | Unofficial, unreliable | Yes (since version 145) | No |
| H.264 | Yes | Yes | Yes |
| HEVC | Hardware decoder required on Windows | Hardware decoder required | Yes on Apple hardware |
| AV1 | Yes | Yes | Recent hardware only |
| AAC | Yes | Yes | Yes |
| AC3 / E-AC3 | No | No | Yes |
| DTS, TrueHD | No | No | No |

- **Multiple audio tracks:** the `audioTracks` API only works in Safari. The practical method is HLS with alternate audio renditions played through hls.js: video is copied into its own playlist, and each audio language gets a separate playlist.
- **Subtitles:**
  - Text subtitles are converted to WebVTT, and their timing can be shifted live through the TextTrack API.
  - Styled ASS subtitles need JASSUB (MIT).
  - Image-based PGS subtitles need libpgs-js.
  - Burning subtitles into the picture is excluded, because it requires video re-encoding.
- **Segmenting with a copied video stream:** segments can only start at keyframes. The playlist must therefore be built from real keyframe times, and it should only list segments whose data is already downloaded. Jellyfin's misaligned-segment problems (issue 17966) show the cost of getting this wrong.
- **Player libraries:**
  - hls.js (Apache-2.0) is the standard engine.
  - Video.js 10 has reached release-candidate stage and merges Video.js, Plyr and Vidstack.
  - Shaka Player is a heavier alternative.

**Decision:** probe each file, then choose the cheapest path:

1. **Direct play:** the container, video codec and selected audio track are all browser-compatible, and the file has a single audio track.
2. **HLS remux:** the video is copied, and the audio is copied when compatible or converted to AAC. This is the default for MKV, for multi-audio files and for files still downloading.
3. **Unsupported video codec:** the user is told the file cannot play in the browser, with an option to open it in mpv. Video is not transcoded.

The player uses hls.js with a small custom interface for audio, subtitle and timing controls. Release scoring prefers H.264 with AAC or AC3 for browser playback.

## 8. Terminal interface

- **Framework:** Bubble Tea 2.x (MIT, stable since February 2026), with the Bubbles components and Lip Gloss.
- **Posters:** Windows Terminal supports sixel images, so posters can be shown where the terminal supports them.
- **Patterns:**
  - Every API call runs as a command that returns a message.
  - One WebSocket delivers events and reconnects with backoff.
  - Search input is debounced and stale responses are dropped.
  - Progress updates are limited to about four per second.

## 9. Consequences for the architecture

1. The browser player comes first for the initial version; mpv integration follows.
2. The playback pipeline is direct play or HLS remux. Video is never transcoded.
3. The torrent engine is embedded, behind an interface that allows replacement.
4. Sources are pluggable: Internet Archive (built in), Torznab (configured by the user) and manual magnet links.
5. Metadata comes from TMDB with a TVmaze fallback, and Trakt is removed.
6. Subtitles come first from inside the file or torrent; online providers are enabled by the user.
7. The terminal interface is built with Bubble Tea.

## Sources

- Torrent engines:
  - anacrolix/torrent: https://github.com/anacrolix/torrent
  - librqbit: https://github.com/ikatson/rqbit
  - libtorrent: https://github.com/arvidn/libtorrent
  - qBittorrent Web API: https://github.com/qbittorrent/qBittorrent/wiki/WebUI-API-(qBittorrent-5.0)
  - Transmission RPC: https://github.com/transmission/transmission/blob/main/docs/rpc-spec.md
  - rain: https://github.com/cenkalti/rain
- Metadata:
  - TMDB terms: https://www.themoviedb.org/api-terms-of-use
  - TVmaze API: https://www.tvmaze.com/api
  - TheTVDB: https://www.thetvdb.com/api-information
  - Trakt limits: https://forums.trakt.tv/t/updating-trakt-limits-for-2026/101592
- Sources:
  - Internet Archive torrents: https://help.archive.org/help/archive-bittorrents/
  - Internet Archive search: https://archive.org/help/aboutsearch.htm
  - Free sample torrents: https://webtorrent.io/free-torrents
  - Torznab specification: https://torznab.github.io/spec-1.3-draft/torznab/Specification-v1.3.html
- Parsing:
  - jhin: https://github.com/dreulavelle/jhin
  - guessit: https://github.com/guessit-io/guessit
- Subtitles:
  - OpenSubtitles API: https://opensubtitles.stoplight.io/docs/opensubtitles-api/e3750fd63a100-getting-started
  - SubDL: https://subdl.com/api-doc
- Playback:
  - Firefox MKV: https://bugzilla.mozilla.org/show_bug.cgi?id=1422891
  - audioTracks support: https://caniuse.com/mdn-api_htmlmediaelement_audiotracks
  - Jellyfin segment issue: https://github.com/jellyfin/jellyfin/issues/17966
  - JASSUB: https://github.com/ThaUnknown/jassub
  - Video.js 10: https://videojs.org/blog/videojs-v10-release-candidate
  - Seanime media streaming: https://seanime.app/docs/mediastream
- Terminal interface:
  - Bubble Tea release notes: https://github.com/charmbracelet/bubbletea/releases/tag/v2.0.0
  - Bubbles release notes: https://github.com/charmbracelet/bubbles/releases/tag/v2.0.0
  - Sixel support: https://www.arewesixelyet.com/
- Reference implementations:
  - webtor.io: https://github.com/webtor-io
  - Seanime: https://github.com/5rahim/seanime
  - stremio-server-go: https://github.com/M0Rf30/stremio-server-go
