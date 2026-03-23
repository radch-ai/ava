-- name: ListAgentRunsFirstPage :many
SELECT id, triggered_by, action, status, message, created_at
FROM agent_runs
WHERE user_id = $1 AND agent_name = $2
ORDER BY created_at ASC, id ASC
LIMIT $3;

-- name: ListAgentRunsAfterCursor :many
SELECT id, triggered_by, action, status, message, created_at
FROM agent_runs
WHERE user_id = $1
  AND agent_name = $2
  AND (
    created_at > $3
    OR (created_at = $3 AND id > $4)
  )
ORDER BY created_at ASC, id ASC
LIMIT $5;
