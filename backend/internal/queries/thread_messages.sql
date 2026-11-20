-- name: ListThreadPageRows :many
-- One thread's visible display rows, newest-first window starting at
-- its newest row. Empty assistant rows (structure-only) are excluded
-- here, before the limit: they never consume a page slot. Visible =
-- role != 'assistant' OR content != ''.
SELECT id, role, content, created_at
FROM thread_messages
WHERE agent_id = ? AND thread_id = ?
  AND (role != 'assistant' OR content != '')
ORDER BY created_at DESC, id DESC
LIMIT ?;

-- name: ListThreadPageRowsBefore :many
-- The same page strictly older than an anchor row of the same thread
-- (keyset continuation on (created_at, id)), covered by
-- idx_thread_messages_thread. The caller resolves the anchor row and
-- binds its (created_at, id) as a row value so sqlite can seek.
-- Visibility filters before the limit, exactly like the entry read.
SELECT id, role, content, created_at
FROM thread_messages
WHERE agent_id = sqlc.arg(agent_id) AND thread_id = sqlc.arg(thread_id)
  AND (created_at, id) < (CAST(sqlc.arg(before_created_at) AS INTEGER), CAST(sqlc.arg(before_id) AS TEXT))
  AND (role != 'assistant' OR content != '')
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(limit);

-- name: GetThreadMessageMeta :one
-- Cursor anchor validation: the message must be a visible row of
-- this thread. sql.ErrNoRows means invalid cursor.
SELECT created_at, id FROM thread_messages
WHERE agent_id = ? AND thread_id = ? AND id = ?
  AND (role != 'assistant' OR content != '');

-- name: ListActiveThreadRows :many
-- The given thread's rows -- model replay scope, fed the agent's
-- active pointer by the caller.
SELECT id, role, content, created_at, turn_id, tool_calls
FROM thread_messages
WHERE agent_id = ? AND thread_id = ?
ORDER BY created_at ASC, id ASC;

-- name: InsertThreadMessage :exec
-- One turn message; the caller mints the id and the ms timestamp.
-- turn_id groups the queue item's rows; tool_calls is the assistant
-- row's model-authored call group (NULL otherwise); thread_id is the
-- active thread the sink resolves at write time.
INSERT INTO thread_messages (id, agent_id, role, content, created_at, turn_id, tool_calls, thread_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);
