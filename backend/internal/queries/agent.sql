-- name: CreateAgent :exec
-- User-created agents; system agents are seeded separately.
-- created_at is DB-stamped.
INSERT INTO agents (id, name, icon, soul_personality, soul_identity, user_context, operating_instructions, tools_context)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetAgentID :one
-- Collision check for CreateAgent's 409 response; sql.ErrNoRows
-- means the id is free.
SELECT id FROM agents WHERE id = ?;

-- name: GetAgentConfig :one
-- The full agent row - the save surface's read step. System rows
-- ("_"-prefixed) are included: the superagent is served too.
SELECT * FROM agents WHERE id = ?;

-- name: UpdateAgentConfig :exec
-- Full-row rewrite of the mutable columns; the save surface does
-- read-modify-write in Go, so every column is written every time.
UPDATE agents
SET name = ?, icon = ?, soul_personality = ?, soul_identity = ?,
    user_context = ?, operating_instructions = ?, tools_context = ?,
    tools_enabled = ?
WHERE id = ?;
