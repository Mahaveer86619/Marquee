-- Release candidates found by release searches.

-- name: DeleteReleaseCandidates :exec
DELETE FROM release_candidates WHERE target_key = ?;

-- name: InsertReleaseCandidate :exec
INSERT INTO release_candidates (
  id, target_key, source, name, info_hash, score, seeders, size_bytes, data, fetched_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListReleaseCandidates :many
SELECT data, fetched_at
FROM release_candidates
WHERE target_key = ?
ORDER BY score DESC, seeders DESC;

-- name: GetReleaseCandidate :one
-- A release may be cached under several searches; the newest copy wins.
SELECT data FROM release_candidates WHERE id = ? ORDER BY fetched_at DESC LIMIT 1;
