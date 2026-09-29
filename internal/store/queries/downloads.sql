-- Downloads and the files chosen from each torrent.

-- name: InsertDownload :exec
INSERT INTO downloads (
  id, title_ref, target_key, scope, season, episode, release_id, release, name,
  info_hash, state, audio, subtitles, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetDownload :one
SELECT * FROM downloads WHERE id = ?;

-- name: ListDownloads :many
SELECT * FROM downloads ORDER BY created_at DESC LIMIT ?;

-- name: ListUnfinishedDownloads :many
-- Downloads to resume when the core starts.
SELECT * FROM downloads
WHERE state IN ('queued', 'metadata', 'downloading', 'paused', 'finalizing')
ORDER BY created_at;

-- name: SetDownloadState :exec
UPDATE downloads SET state = ?, error = ?, updated_at = ? WHERE id = ?;

-- name: SetDownloadInfo :exec
-- Called once the torrent metadata is known.
UPDATE downloads SET info_hash = ?, name = ?, bytes_total = ?, updated_at = ? WHERE id = ?;

-- name: SetDownloadProgress :exec
UPDATE downloads SET bytes_done = ?, bytes_total = ?, updated_at = ? WHERE id = ?;

-- name: CompleteDownload :exec
UPDATE downloads SET state = 'completed', error = NULL, bytes_done = bytes_total,
  completed_at = ?, updated_at = ? WHERE id = ?;

-- name: DeleteDownloadFiles :exec
DELETE FROM download_files WHERE download_id = ?;

-- name: InsertDownloadFile :exec
INSERT INTO download_files (
  download_id, file_index, path, size_bytes, kind, language, item_ref, selected
) VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListDownloadFiles :many
SELECT * FROM download_files WHERE download_id = ? ORDER BY file_index;

-- name: SetDownloadFileProgress :exec
UPDATE download_files SET bytes_done = ? WHERE download_id = ? AND file_index = ?;

-- name: SetDownloadFileLibraryPath :exec
UPDATE download_files SET library_path = ?, bytes_done = size_bytes WHERE download_id = ? AND file_index = ?;
