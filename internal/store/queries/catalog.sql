-- Catalog queries: titles, seasons and playable items.
-- Each query is named for the generated Go method. Paste any of them into a
-- SQLite shell against core.db to debug (replace ? with values).

-- name: UpsertTitle :one
-- Inserts a title or updates it in place, keeping its id. Returns the id.
INSERT INTO titles (
  id, ref, kind, source, tmdb_id, tvmaze_id, tvdb_id, imdb_id,
  name, original_name, year, overview, status, runtime_min,
  genres, poster_url, details, fetched_at
) VALUES (
  ?, ?, ?, ?, ?, ?, ?, ?,
  ?, ?, ?, ?, ?, ?,
  ?, ?, ?, ?
)
ON CONFLICT (ref) DO UPDATE SET
  kind          = excluded.kind,
  source        = excluded.source,
  tmdb_id       = excluded.tmdb_id,
  tvmaze_id     = excluded.tvmaze_id,
  tvdb_id       = excluded.tvdb_id,
  imdb_id       = excluded.imdb_id,
  name          = excluded.name,
  original_name = excluded.original_name,
  year          = excluded.year,
  overview      = excluded.overview,
  status        = excluded.status,
  runtime_min   = excluded.runtime_min,
  genres        = excluded.genres,
  poster_url    = excluded.poster_url,
  details       = excluded.details,
  fetched_at    = excluded.fetched_at
RETURNING id;

-- name: GetTitleByRef :one
SELECT * FROM titles WHERE ref = ?;

-- name: GetTitleIDByRef :one
SELECT id FROM titles WHERE ref = ?;

-- name: UpsertSeason :exec
INSERT INTO seasons (title_id, season_number, name, air_date, episode_count)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (title_id, season_number) DO UPDATE SET
  name          = excluded.name,
  air_date      = excluded.air_date,
  episode_count = excluded.episode_count;

-- name: ListSeasons :many
SELECT season_number, name, air_date, episode_count
FROM seasons
WHERE title_id = ?
ORDER BY season_number;

-- name: UpsertMovieItem :exec
INSERT INTO items (id, title_id, kind, name, overview, runtime_min, ref)
VALUES (?, ?, 'movie', ?, ?, ?, ?)
ON CONFLICT (ref) DO UPDATE SET
  name        = excluded.name,
  overview    = excluded.overview,
  runtime_min = excluded.runtime_min;

-- name: UpsertEpisode :exec
INSERT INTO items (
  id, title_id, kind, season_number, episode_number,
  name, overview, air_date, runtime_min, rating, ref
) VALUES (
  ?, ?, 'episode', ?, ?,
  ?, ?, ?, ?, ?, ?
)
ON CONFLICT (ref) DO UPDATE SET
  name        = excluded.name,
  overview    = excluded.overview,
  air_date    = excluded.air_date,
  runtime_min = excluded.runtime_min,
  rating      = excluded.rating;

-- name: MarkSeasonEpisodesFetched :exec
-- Records when a season's episodes were fetched (creates the season row if needed).
INSERT INTO seasons (title_id, season_number, episode_count, episodes_fetched_at)
VALUES (?, ?, ?, ?)
ON CONFLICT (title_id, season_number) DO UPDATE SET
  episodes_fetched_at = excluded.episodes_fetched_at;

-- name: GetSeasonEpisodesFetchedAt :one
SELECT se.episodes_fetched_at
FROM seasons se
JOIN titles t ON t.id = se.title_id
WHERE t.ref = ? AND se.season_number = ?;

-- name: ListEpisodes :many
SELECT i.ref, i.season_number, i.episode_number, i.name, i.overview,
       i.air_date, i.runtime_min, i.rating
FROM items i
JOIN titles t ON t.id = i.title_id
WHERE t.ref = ? AND i.kind = 'episode' AND i.season_number = ?
ORDER BY i.episode_number;
