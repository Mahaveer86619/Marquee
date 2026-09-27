# Third-party software

Marquee's own source code is licensed under the MIT License. It relies on the following third-party software, which keeps its own license. This list will grow as dependencies are added.

## Used at runtime

| Component | Where | License | Notes |
|---|---|---|---|
| ffmpeg / ffprobe | Container images (`core`, `audiolab`) | LGPL-2.1 or later; the Debian build includes GPL components | Installed from Debian packages and run as separate processes. Marquee does not link against ffmpeg libraries. Source code is available from Debian. |
| Prowlarr | Optional container (`indexers` profile), image `lscr.io/linuxserver/prowlarr` | GPL-3.0 | Pulled from its publisher at install time and run as a separate service. Not modified or redistributed by Marquee. |
| mpv | Host, installed by the user | GPL-2.0 or later / LGPL-2.1 or later | Not distributed with Marquee. Controlled over its IPC interface. |
| Debian base image | Container images | Various free software licenses | `debian:stable-slim` |
| Python | Container images | PSF License | `python:3.12-slim` |

## Planned dependencies

| Component | License |
|---|---|
| anacrolix/torrent | MPL-2.0 |
| modernc.org/sqlite | BSD-3-Clause |
| Bubble Tea, Lip Gloss | MIT |
| go-libp2p | MIT / Apache-2.0 |
| tailscale.com/tsnet | BSD-3-Clause |
| rclone | MIT |
| faster-whisper, CTranslate2 | MIT |
| Silero VAD | MIT |
| ONNX Runtime | MIT |

Model weights are downloaded at runtime and carry their own licenses. Only models whose licenses permit commercial use are selected by default.

## Container image distribution

If pre-built container images are published, they redistribute ffmpeg and other Debian packages. Published images must include the corresponding license notices, and must reference the source for the exact package versions used.
