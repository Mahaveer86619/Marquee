-- Release search results, kept for an hour so a release can be chosen by id.
-- target_key is "<ref>|<scope>" (e.g. "tmdb:tv:84958:s01e02|episode").
-- data holds the full release as JSON (parsed name, files, score reasons).

CREATE TABLE release_candidates (
  id          TEXT PRIMARY KEY,
  target_key  TEXT NOT NULL,
  source      TEXT NOT NULL,
  name        TEXT NOT NULL,
  info_hash   TEXT,
  score       INTEGER NOT NULL,
  seeders     INTEGER NOT NULL,
  size_bytes  INTEGER,
  data        TEXT NOT NULL CHECK (json_valid(data)),
  fetched_at  INTEGER NOT NULL
) STRICT;
CREATE INDEX ix_release_candidates_target ON release_candidates(target_key, score DESC, seeders DESC);
