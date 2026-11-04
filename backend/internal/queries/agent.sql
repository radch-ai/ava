-- name: CreateAgent :exec
-- User-created agents; system agents are seeded separately.
-- created_at is DB-stamped.
INSERT INTO agents (id, name, icon, soul_personality, soul_identity, user_context, operating_instructions, tools_context)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetAgentID :one
-- Collision check for CreateAgent's 409 response; sql.ErrNoRows
-- means the id is free.
SELECT id FROM agents WHERE id = ?;
