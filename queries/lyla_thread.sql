-- name: ListLylaThreadFirstPage :many
SELECT id, role, content, created_at
FROM lyla_thread
WHERE user_id = $1
ORDER BY created_at ASC, id ASC
LIMIT $2;

-- name: ListLylaThreadAfterCursor :many
SELECT id, role, content, created_at
FROM lyla_thread
WHERE user_id = $1
  AND (
    created_at > $2
    OR (created_at = $2 AND id > $3)
  )
ORDER BY created_at ASC, id ASC
LIMIT $4;
