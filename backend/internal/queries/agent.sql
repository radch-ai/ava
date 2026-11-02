-- name: CreateAgent :exec
-- User-created agents only; the _superagent seed comes from
-- migrations, never here. created_at is DB-stamped (0001).
INSERT INTO agents (id, name, icon, config)
VALUES (?, ?, ?, ?);

-- name: GetAgentID :one
-- Collision check for CreateAgent's 409 response; sql.ErrNoRows
-- means the id is free.
SELECT id FROM agents WHERE id = ?;
