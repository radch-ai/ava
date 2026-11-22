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
-- globally off (the switch is opted in, never assumed) while the
-- update path refreshes metadata -- name, description and definition --
-- without touching the saved global switch or any agent's opt-out.
INSERT INTO tools (id, name, description, builtin, enabled, kind, server_id, definition_json)
VALUES (?, ?, ?, 0, 0, 'mcp', ?, ?)
ON CONFLICT(id) DO UPDATE SET
    name = excluded.name,
    description = excluded.description,
    definition_json = excluded.definition_json;

-- name: DeleteVanishedMcpTools :execrows
-- Prune catalog rows the server no longer reports; their opt-outs
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

-- name: AddAgentDisabledTool :exec
-- The agent opts out of one globally enabled tool.
INSERT OR IGNORE INTO agent_disabled_tools (agent_id, tool_id) VALUES (?, ?);

-- name: RemoveAgentDisabledTool :exec
-- The agent inherits the tool again.
DELETE FROM agent_disabled_tools WHERE agent_id = ? AND tool_id = ?;

-- name: ListAgentTools :many
-- One row per installed tool as the agent sees it: the global
-- switch, and whether this agent opted out.
SELECT t.id, t.name, t.description, t.kind, t.server_id,
       t.enabled AS globally_enabled,
       EXISTS(SELECT 1 FROM agent_disabled_tools d
              WHERE d.agent_id = ? AND d.tool_id = t.id) AS locally_disabled
FROM tools t
ORDER BY t.created_at, t.id;

-- name: GetAgentTool :one
-- The ListAgentTools row for one tool.
SELECT t.id, t.name, t.description, t.kind, t.server_id,
       t.enabled AS globally_enabled,
       EXISTS(SELECT 1 FROM agent_disabled_tools d
              WHERE d.agent_id = ? AND d.tool_id = t.id) AS locally_disabled
FROM tools t
WHERE t.id = ?;

-- name: ResolveAgentToolsDetailed :many
-- The turn's effective tool set: globally enabled tools the agent has
-- not opted out of, minus MCP tools whose server is switched off
-- globally, paused, or whose per-agent gate is not on. Builtins have
-- no server and no gate -- global switch alone.
SELECT t.id, t.server_id FROM tools t
LEFT JOIN mcp_servers s ON s.id = t.server_id
LEFT JOIN agent_mcp_preferences ap
    ON ap.server_id = t.server_id AND ap.agent_id = ?
WHERE t.enabled = 1
  AND NOT EXISTS (SELECT 1 FROM agent_disabled_tools d
                  WHERE d.agent_id = ? AND d.tool_id = t.id)
  AND (t.server_id IS NULL OR (s.enabled = 1 AND s.catalog_state = 'active' AND ap.enabled = 1))
ORDER BY t.id;

-- name: ListMcpServerTools :many
-- One server's catalog rows: the response listing when an edit skipped
-- discovery (rename-only patches).
SELECT * FROM tools WHERE server_id = ? ORDER BY created_at, id;

-- name: GetAgentMcp :one
-- One agent's parent gate row for one server; sql.ErrNoRows = off.
SELECT * FROM agent_mcp_preferences WHERE agent_id = ? AND server_id = ?;

-- name: SetAgentMcpEnabled :execrows
-- Flip the parent gate, creating the row when absent. A pure state
-- write: tools are never seeded or revoked by a gate toggle.
INSERT INTO agent_mcp_preferences (agent_id, server_id, enabled)
VALUES (?, ?, ?)
ON CONFLICT(agent_id, server_id) DO UPDATE SET enabled = excluded.enabled;

-- name: ListAgentMcps :many
-- Every parent gate row this agent holds, for the editor's server
-- switches (absent server = off).
SELECT * FROM agent_mcp_preferences WHERE agent_id = ? ORDER BY server_id;

-- name: ListOtherServerToolNames :many
-- Every other server's tool names, for the model-name collision check at
-- publish time: two servers whose composites collide cannot both be
-- advertised.
SELECT t.name, t.server_id FROM tools t
WHERE t.server_id IS NOT NULL AND t.server_id != ?;

-- name: AuthorizeToolCall :one
-- The per-invocation authorization fold: the current global switches,
-- the owning server's switch and catalog state, the agent's parent gate
-- and explicit opt-out. The caller applies the denial precedence.
SELECT t.server_id,
       t.enabled AS tool_enabled,
       CAST(COALESCE(s.enabled, 1) AS INTEGER) AS server_enabled,
       CAST(COALESCE(s.catalog_state, 'active') AS TEXT) AS catalog_state,
       -- The agent's parent-server gate applies to MCP tools only: a
       -- builtin has no server row and must execute on its global switch
       -- and opt-out alone.
       CAST(CASE WHEN t.server_id IS NULL THEN 1 ELSE COALESCE(ap.enabled, 0) END AS INTEGER) AS gate_on,
       EXISTS(SELECT 1 FROM agent_disabled_tools d
              WHERE d.agent_id = ? AND d.tool_id = t.id) AS locally_disabled
FROM tools t
LEFT JOIN mcp_servers s ON s.id = t.server_id
LEFT JOIN agent_mcp_preferences ap ON ap.server_id = t.server_id AND ap.agent_id = ?
WHERE t.id = ?;
