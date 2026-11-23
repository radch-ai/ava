-- MCP server rows: CRUD + discovery diagnostics. Secret values never
-- flow through these queries -- the table holds key names only.

-- name: CreateMcpServer :exec
-- id (slug) is stamped by the caller; created_at defaults in the DB.
-- Connection is verified by the API before this runs.
INSERT INTO mcp_servers (id, name, url, header_keys, timeout_ms,
                         allow_private_network, allow_plaintext)
VALUES (?, ?, ?, ?, ?, ?, ?);

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
SET name = ?, url = ?, header_keys = ?, timeout_ms = ?,
    allow_private_network = ?, allow_plaintext = ?
WHERE id = ?;

-- name: ToggleMcpServerEnabled :execrows
-- Flip the server-level kill switch; zero rows = unknown server.
UPDATE mcp_servers SET enabled = ? WHERE id = ?;

-- name: DeleteMcpServer :execrows
-- Cascades the server's tools and, through them, every agent opt-out.
DELETE FROM mcp_servers WHERE id = ?;

-- name: SetMcpServerState :execrows
-- Catalog admission state: paused after a security-rejected
-- generation, active after a valid one. The tools themselves stay put.
UPDATE mcp_servers SET catalog_state = ? WHERE id = ?;

-- name: SetMcpServerDiagnostics :exec
-- Post-discovery state: last error (NULL = healthy).
UPDATE mcp_servers SET last_error = ? WHERE id = ?;

-- name: ListMcpServersWithCounts :many
-- Servers in creation order with their catalog tool counts: one query,
-- no per-row count round-trip.
SELECT sqlc.embed(s), COUNT(t.id) AS tool_count
FROM mcp_servers s
LEFT JOIN tools t ON t.server_id = s.id
GROUP BY s.id
ORDER BY s.created_at, s.id;
