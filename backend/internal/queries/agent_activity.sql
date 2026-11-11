-- name: ListAgentActivityFirstPage :many
-- The newest page of the agent's activity feed; id breaks ties for a
-- stable order.
SELECT id, triggered_by, action, status, message, created_at
FROM agent_activity
WHERE agent_id = ?
ORDER BY created_at DESC, id DESC
LIMIT ?;

-- name: ListAgentActivityAfterCursor :many
-- The page strictly older than the cursor row.
SELECT id, triggered_by, action, status, message, created_at
FROM agent_activity
WHERE agent_id = ?
  AND (created_at < ? OR (created_at = ? AND id < ?))
ORDER BY created_at DESC, id DESC
LIMIT ?;

-- name: GetAgentActivityItem :one
-- Cursor validation: the id must name one of this agent's rows.
SELECT id, created_at
FROM agent_activity
WHERE id = ? AND agent_id = ?;
