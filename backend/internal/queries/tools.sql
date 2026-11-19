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

-- name: UpsertMcpTool :exec
-- Discovery sync for one of a server's tools: a fresh insert lands
-- un-granted -- builtin=0 defeats the seed trigger, so MCP tools
-- install default-deny -- while the update path refreshes metadata
-- without touching the saved global switch or grants.
INSERT INTO tools (id, name, description, builtin, kind, server_id)
VALUES (?, ?, ?, 0, 'mcp', ?)
ON CONFLICT(id) DO UPDATE SET
    name = excluded.name,
    description = excluded.description;

-- name: DeleteVanishedMcpTools :execrows
-- Prune catalog rows the server no longer reports; their grants
-- cascade away with them. The parameter is the JSON array of kept
-- tool ids (json_each), so an empty list prunes everything.
DELETE FROM tools
WHERE server_id = ?
  AND id NOT IN (SELECT value FROM json_each(CAST(sqlc.arg('kept_ids') AS TEXT)));

-- name: CountMcpServerTools :one
-- Tool count for server cards and refresh diagnostics.
SELECT COUNT(*) FROM tools WHERE server_id = ?;

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
SELECT t.id, t.name, t.description, t.kind, t.server_id,
       t.enabled AS globally_enabled,
       CAST(p.agent_id IS NOT NULL AS INTEGER) AS agent_enabled
FROM tools t
LEFT JOIN agent_tool_preferences p ON p.tool_id = t.id AND p.agent_id = ?
ORDER BY t.created_at, t.id;

-- name: GetAgentTool :one
-- The ListAgentTools row for one tool.
SELECT t.id, t.name, t.description, t.kind, t.server_id,
       t.enabled AS globally_enabled,
       CAST(p.agent_id IS NOT NULL AS INTEGER) AS agent_enabled
FROM tools t
LEFT JOIN agent_tool_preferences p ON p.tool_id = t.id AND p.agent_id = ?
WHERE t.id = ?;

-- name: ResolveAgentTools :many
-- The turn's effective tool set: globally enabled tools the agent
-- holds a preference row for, minus MCP tools whose server is
-- switched off globally or whose per-agent gate is not on. Builtins
-- have no server and no gate -- tool switch plus grant, as before.
SELECT t.id FROM tools t
JOIN agent_tool_preferences p ON p.tool_id = t.id AND p.agent_id = ?
LEFT JOIN mcp_servers s ON s.id = t.server_id
LEFT JOIN agent_mcp_preferences ap
    ON ap.server_id = t.server_id AND ap.agent_id = ?
WHERE t.enabled = 1
  AND (t.server_id IS NULL OR (s.enabled = 1 AND ap.enabled = 1))
ORDER BY t.id;

-- name: ListMcpServerTools :many
-- One server's catalog rows: the response listing when an edit skipped
-- discovery (rename-only patches).
SELECT * FROM tools WHERE server_id = ? ORDER BY created_at, id;

-- name: GetAgentMcp :one
-- One agent's parent gate row for one server; sql.ErrNoRows = never
-- approved (default-deny, distinct from a configured off).
SELECT * FROM agent_mcp_preferences WHERE agent_id = ? AND server_id = ?;

-- name: SetAgentMcpEnabled :execrows
-- Flip the parent gate, creating the row when absent. Seeding the
-- first enable's tool grants is the caller's decision, so this
-- stays a pure state write.
INSERT INTO agent_mcp_preferences (agent_id, server_id, enabled)
VALUES (?, ?, ?)
ON CONFLICT(agent_id, server_id) DO UPDATE SET enabled = excluded.enabled;

-- name: SeedAgentMcpTools :execrows
-- First enable: approve the server's currently listed tools in
-- one statement. Rows that exist keep their state (OR IGNORE).
INSERT OR IGNORE INTO agent_tool_preferences (agent_id, tool_id)
SELECT ?, id FROM tools WHERE server_id = ?;

-- name: ListAgentMcps :many
-- Every parent gate row this agent holds, for the editor's server
-- switches (absent server = never approved).
SELECT * FROM agent_mcp_preferences WHERE agent_id = ? ORDER BY server_id;
