-- Catalog: metadata cache for titles, seasons and playable items.
-- See notes/SCHEMA.md section 2.2.

CREATE TABLE titles (
  id             TEXT PRIMARY KEY,                    -- UUIDv7
  ref            TEXT NOT NULL UNIQUE,                -- tmdb:movie:603, tmdb:tv:1399, tvmaze:show:82
  kind           TEXT NOT NULL CHECK (kind IN ('movie','series')),
  source         TEXT NOT NULL,                       -- provider the data came from
  tmdb_id        INTEGER,
  tvmaze_id      INTEGER,
  tvdb_id        INTEGER,
  imdb_id        TEXT,
  name           TEXT NOT NULL,
  original_name  TEXT,
  year           INTEGER,
  overview       TEXT,
  status         TEXT,
  runtime_min    INTEGER,
  genres         TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(genres)),
  poster_url     TEXT,
  fetched_at     INTEGER NOT NULL
) STRICT;
CREATE INDEX ix_titles_tmdb ON titles(kind, tmdb_id) WHERE tmdb_id IS NOT NULL;

CREATE TABLE seasons (
  title_id             TEXT NOT NULL REFERENCES titles(id) ON DELETE CASCADE,
  season_number        INTEGER NOT NULL,
  name                 TEXT,
  air_date             TEXT,
  episode_count        INTEGER,
  episodes_fetched_at  INTEGER,
  PRIMARY KEY (title_id, season_number)
) STRICT;

-- One row per film and one per episode.
CREATE TABLE items (
  id              TEXT PRIMARY KEY,
  title_id        TEXT NOT NULL REFERENCES titles(id) ON DELETE CASCADE,
  kind            TEXT NOT NULL CHECK (kind IN ('movie','episode')),
  season_number   INTEGER,
  episode_number  INTEGER,
  name            TEXT,
  overview        TEXT,
  air_date        TEXT,
  runtime_min     INTEGER,
  ref             TEXT NOT NULL UNIQUE,
  CHECK ((kind = 'movie'   AND season_number IS NULL AND episode_number IS NULL) OR
         (kind = 'episode' AND season_number IS NOT NULL AND episode_number IS NOT NULL))
) STRICT;
CREATE UNIQUE INDEX ux_items_movie   ON items(title_id) WHERE kind = 'movie';
CREATE UNIQUE INDEX ux_items_episode ON items(title_id, season_number, episode_number) WHERE kind = 'episode';
