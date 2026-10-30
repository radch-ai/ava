-- name: ListAgents :many
-- The user's agents in creation order. The system superagent is
-- excluded: clients render it from their own constant, never from here.
SELECT id, name, icon
FROM agents
WHERE id != '_superagent'
ORDER BY created_at ASC, id ASC;
