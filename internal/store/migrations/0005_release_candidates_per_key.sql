-- The same release can be cached under several searches (all sources, or one
-- source at a time for progressive results), so the key is (id, target_key).
-- The table only holds search results cached for an hour; rebuilding it is safe.

DROP TABLE release_candidates;

CREATE TABLE release_candidates (
  id          TEXT NOT NULL,
  target_key  TEXT NOT NULL,
  source      TEXT NOT NULL,
  name        TEXT NOT NULL,
  info_hash   TEXT,
  score       INTEGER NOT NULL,
  seeders     INTEGER NOT NULL,
  size_bytes  INTEGER,
  data        TEXT NOT NULL CHECK (json_valid(data)),
  fetched_at  INTEGER NOT NULL,
  PRIMARY KEY (id, target_key)
) STRICT;
CREATE INDEX ix_release_candidates_target ON release_candidates(target_key, score DESC, seeders DESC);
CREATE INDEX ix_release_candidates_id ON release_candidates(id, fetched_at DESC);
