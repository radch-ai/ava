-- name: ListTools :many
-- The catalog in creation order.
SELECT * FROM tools ORDER BY created_at, id;

-- name: GetTool :one
SELECT * FROM tools WHERE id = ?;

-- name: UpsertTool :exec
-- Boot sync: create missing entries with their defaults; refresh
-- code-owned metadata without touching the saved global switch.
INSERT INTO tools (id, name, description, builtin) VALUES (?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    name = excluded.name,
    description = excluded.description,
    builtin = excluded.builtin;

-- name: ToggleToolEnabled :execrows
-- Flip one tool's global switch; zero rows = unknown tool.
UPDATE tools SET enabled = ? WHERE id = ?;

-- name: AddAgentTool :execrows
-- Enable one tool for one agent; a row's presence is the grant.
INSERT OR IGNORE INTO agent_tool_preferences (agent_id, tool_id) VALUES (?, ?);

-- name: RemoveAgentTool :execrows
-- Disable one tool for one agent.
DELETE FROM agent_tool_preferences WHERE agent_id = ? AND tool_id = ?;

-- name: ListAgentTools :many
-- One row per installed tool as the agent sees it: the global
-- switch, and whether the agent holds a preference row.
SELECT t.id, t.name, t.description,
       t.enabled AS globally_enabled,
       CAST(p.agent_id IS NOT NULL AS INTEGER) AS agent_enabled
FROM tools t
LEFT JOIN agent_tool_preferences p ON p.tool_id = t.id AND p.agent_id = ?
ORDER BY t.created_at, t.id;

-- name: GetAgentTool :one
-- The ListAgentTools row for one tool.
SELECT t.id, t.name, t.description,
       t.enabled AS globally_enabled,
       CAST(p.agent_id IS NOT NULL AS INTEGER) AS agent_enabled
FROM tools t
LEFT JOIN agent_tool_preferences p ON p.tool_id = t.id AND p.agent_id = ?
WHERE t.id = ?;

-- name: ResolveAgentTools :many
-- The turn's effective tool set: globally enabled tools the agent
-- holds a preference row for -- one join, no per-agent switch.
SELECT t.id FROM tools t
JOIN agent_tool_preferences p ON p.tool_id = t.id AND p.agent_id = ?
WHERE t.enabled = 1
ORDER BY t.id;
