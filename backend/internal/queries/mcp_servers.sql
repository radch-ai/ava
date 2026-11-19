-- MCP server rows: CRUD + discovery diagnostics. Secret values never
-- flow through these queries -- the table holds key names only.

-- name: CreateMcpServer :exec
-- id (slug) is stamped by the caller; created_at defaults in the DB.
-- Connection is verified by the API before this runs.
INSERT INTO mcp_servers (id, name, transport, command, args, url, env_keys, header_keys, timeout_ms)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListMcpServers :many
-- Servers in creation order, diagnostics included so a down server's
-- inert tools are explainable.
SELECT * FROM mcp_servers ORDER BY created_at, id;

-- name: GetMcpServer :one
-- Row for one server; sql.ErrNoRows = unknown id.
SELECT * FROM mcp_servers WHERE id = ?;

-- name: UpdateMcpServer :exec
-- Whole-config replace; connection-affecting edits are re-verified
-- by the API before this runs. enabled is deliberately not here --
-- the kill switch has its own query.
UPDATE mcp_servers
SET name = ?, transport = ?, command = ?, args = ?, url = ?, env_keys = ?, header_keys = ?, timeout_ms = ?
WHERE id = ?;

-- name: ToggleMcpServerEnabled :execrows
-- Flip the server-level kill switch; zero rows = unknown server.
UPDATE mcp_servers SET enabled = ? WHERE id = ?;

-- name: DeleteMcpServer :execrows
-- Cascades the server's tools and, through them, every grant they held.
DELETE FROM mcp_servers WHERE id = ?;

-- name: SetMcpServerDiagnostics :exec
-- Post-discovery state: last error (NULL = healthy) and when the
-- server's tools were last synced into the catalog.
UPDATE mcp_servers SET last_error = ?, refreshed_at = ? WHERE id = ?;

-- name: ListMcpServersWithCounts :many
-- Servers in creation order with their catalog tool counts: one query,
-- no per-row count round-trip.
SELECT sqlc.embed(s), COUNT(t.id) AS tool_count
FROM mcp_servers s
LEFT JOIN tools t ON t.server_id = s.id
GROUP BY s.id
ORDER BY s.created_at, s.id;
