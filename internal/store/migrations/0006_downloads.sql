-- Downloads: one row per queued release, with the files chosen from it.
-- The release is stored as a snapshot so a download never depends on the
-- one-hour search cache.

CREATE TABLE downloads (
  id            TEXT PRIMARY KEY,                    -- UUIDv7
  title_ref     TEXT NOT NULL,                       -- film or series reference
  target_key    TEXT NOT NULL,                       -- "<ref>|<scope>" of the search
  scope         TEXT NOT NULL CHECK (scope IN ('movie','episode','season','series')),
  season        INTEGER,
  episode       INTEGER,
  release_id    TEXT NOT NULL,
  release       TEXT NOT NULL CHECK (json_valid(release)),
  name          TEXT NOT NULL,
  info_hash     TEXT,
  state         TEXT NOT NULL CHECK (state IN ('queued','metadata','downloading','paused','finalizing','completed','failed','cancelled')),
  audio         TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(audio)),      -- ISO 639-1 codes to keep, or ["*"]
  subtitles     TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(subtitles)),
  bytes_done    INTEGER NOT NULL DEFAULT 0,
  bytes_total   INTEGER NOT NULL DEFAULT 0,
  error         TEXT,
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL,
  completed_at  INTEGER
) STRICT;
CREATE INDEX ix_downloads_state ON downloads(state, created_at);

CREATE TABLE download_files (
  download_id   TEXT NOT NULL REFERENCES downloads(id) ON DELETE CASCADE,
  file_index    INTEGER NOT NULL,                    -- index in the torrent
  path          TEXT NOT NULL,                       -- path inside the torrent
  size_bytes    INTEGER NOT NULL,
  kind          TEXT NOT NULL CHECK (kind IN ('video','subtitle','audio','other')),
  language      TEXT,                                -- ISO 639-1 for subtitle/audio files
  item_ref      TEXT,                                -- film or episode the file belongs to
  selected      INTEGER NOT NULL CHECK (selected IN (0,1)),
  bytes_done    INTEGER NOT NULL DEFAULT 0,
  library_path  TEXT,                                -- final location, relative to the library
  PRIMARY KEY (download_id, file_index)
) STRICT;
