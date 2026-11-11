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

-- name: GetThreadMessage :one
-- Cursor validation: the id must name one of this agent's messages.
SELECT id, created_at
FROM thread_messages
WHERE id = ? AND agent_id = ?;
