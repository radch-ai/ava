-- name: ListThreadFirstPage :many
SELECT id, role, content, created_at
FROM thread
WHERE user_id = $1
ORDER BY created_at ASC, id ASC
LIMIT $2;

-- name: ListThreadAfterCursor :many
SELECT id, role, content, created_at
FROM thread
WHERE user_id = $1
  AND (
    created_at > $2
    OR (created_at = $2 AND id > $3)
  )
ORDER BY created_at ASC, id ASC
LIMIT $4;
