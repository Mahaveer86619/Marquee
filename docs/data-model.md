# Data model

Marquee stores all state in SQLite. This document summarizes the planned schema. Table definitions are introduced through migrations as each feature is built.

## Conventions

- **Engine:** SQLite in WAL mode with `STRICT` tables and foreign keys enabled.
- **Ownership:** one writing service per database; no queries across databases.
- **Identifiers:** UUIDv7 text keys for rows that may be referenced across machines.
- **Universal references:** anything shared between installations identifies content by TMDB reference, for example `tmdb:movie:603` or `tmdb:tv:1399:s01e01`. Local identifiers never leave the machine.
- **Time:** Unix epoch milliseconds in UTC.
- **Profiles:** every per-person table carries a `profile_id`, so several people can share one installation.

## core.db

| Group | Tables |
|---|---|
| Profiles | `profiles`, `profile_prefs` |
| Catalog | `titles`, `seasons`, `items` (one row per film or episode), `watch_providers` |
| Library | `volumes`, `media_files`, `media_tracks`, `file_versions` |
| Downloads | `torrents`, `torrent_files`, `release_candidates`, `download_queue` |
| Subtitles | `subtitles`, `subtitle_sync`, `timeline_refs` |
| Viewing | `watch_history`, `resume_points`, `ratings`, `watchlist`, `follows`, `dismissals`, `pins`, `recaps`, `audio_profiles` |
| Storage | `storage_actions`, `trash`, `stems_cache` |
| System | `jobs`, `settings`, `providers_config`, `secrets`, `hardware_profiles`, `backup_targets`, `backups` |

## recs.db

| Group | Tables |
|---|---|
| Items | `item_features`, `vec_items` (vector index), `item_vec_map` |
| Signals | `interactions`, `impressions`, `friend_signals`, `friend_affinity` |
| Models | `taste_profiles`, `candidates`, `rewatch_predictions`, `model_runs` |

## social.db

| Group | Tables |
|---|---|
| Identity | `identities` (one key pair per profile), `link_transports` |
| Friends | `peers`, `friends`, `friend_scopes`, `invites` |
| Feed | `feed_entries` (signed, append-only), `feed_cursors`, `outbox` |
| Co-watch | `rooms`, `room_members`, `messages`, `reactions` |

## What is shared with friends

Only viewing metadata the user has enabled for a given friend is shared:

| Data | Scope |
|---|---|
| Finished titles | activity |
| Ratings and reviews | ratings, reviews |
| Watchlist additions | watchlist |
| Taste summary | taste_summary |
| Currently watching | now_watching (live, not stored) |
| Co-watch invitations | cowatch_invites |

New friendships start with every scope disabled except co-watch invitations.

Files, file paths, release names, torrent identifiers, indexer settings, storage details and hardware information are never shared.
