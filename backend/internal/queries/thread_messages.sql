-- name: ListThreadFirstPage :many
-- The newest page of the agent's thread; id breaks ties for a
-- stable order.
SELECT id, role, content, created_at
FROM thread_messages
WHERE agent_id = ?
ORDER BY created_at DESC, id DESC
LIMIT ?;

-- name: ListThreadAfterCursor :many
-- The page strictly older than the cursor row.
SELECT id, role, content, created_at
FROM thread_messages
WHERE agent_id = ?
  AND (created_at < ? OR (created_at = ? AND id < ?))
ORDER BY created_at DESC, id DESC
LIMIT ?;

-- name: InsertThreadMessage :exec
-- One turn message; the caller mints the id and the ms timestamp.
INSERT INTO thread_messages (id, agent_id, role, content, created_at)
VALUES (?, ?, ?, ?, ?);

-- name: GetThreadMessage :one
-- Cursor validation: the id must name one of this agent's messages.
SELECT id, created_at
FROM thread_messages
WHERE id = ? AND agent_id = ?;
