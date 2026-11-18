-- name: ListThreadRows :many
-- Every row of the agent's thread, oldest first: the one raw read
-- behind both the display page projection and the model-input
-- projection, which walk these rows in memory.
SELECT id, role, content, created_at, turn_id, tool_calls
FROM thread_messages
WHERE agent_id = ?
ORDER BY created_at ASC, id ASC;

-- name: InsertThreadMessage :exec
-- One turn message; the caller mints the id and the ms timestamp.
-- turn_id groups the queue item's rows; tool_calls is the assistant
-- row's model-authored call group (NULL otherwise).
INSERT INTO thread_messages (id, agent_id, role, content, created_at, turn_id, tool_calls)
VALUES (?, ?, ?, ?, ?, ?, ?);
