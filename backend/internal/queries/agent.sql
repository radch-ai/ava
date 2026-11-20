-- name: CreateAgent :exec
-- User-created agents; system agents are seeded separately.
-- created_at is DB-stamped. The caller mints the initial thread id,
-- names it here, and inserts the thread in the same transaction --
-- the pointer FK is deferred so the agent row lands first.
INSERT INTO agents (id, name, icon, soul_personality, soul_identity, user_context, operating_instructions, tools_context, active_thread_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: InsertThread :exec
-- One conversation boundary; created_at is DB-stamped. The owning
-- agent row must already exist (immediate FK). previous_thread_id
-- names the formerly active thread; NULL on the eagerly created
-- initial thread.
INSERT INTO threads (id, agent_id, previous_thread_id) VALUES (?, ?, ?);

-- name: CreateThreadFromActive :one
-- One conversation boundary linked to the agent's current active
-- thread as its predecessor, in a single statement: the read and the
-- insert share a statement, so the chain cannot name a stale
-- pointer. sql.ErrNoRows means unknown agent; the caller then swaps
-- the pointer. created_at is DB-stamped and returned for the
-- completion event.
INSERT INTO threads (id, agent_id, previous_thread_id)
SELECT sqlc.arg(thread_id), a.id, a.active_thread_id
FROM agents a WHERE a.id = sqlc.arg(agent_id)
RETURNING previous_thread_id, created_at;

-- name: GetThreadMeta :one
-- Display-navigation read: predecessor + creation time of one
-- thread. sql.ErrNoRows means the thread does not exist for this
-- agent.
SELECT previous_thread_id, created_at FROM threads
WHERE agent_id = ? AND id = ?;

-- name: GetActiveThreadMeta :one
-- Pointer resolve that also carries the predecessor + created_at;
-- the display endpoint's no-cursor entry read. sql.ErrNoRows means
-- unknown agent. The join constrains both rows to one agent, so the
-- id binds once.
SELECT t.id, t.previous_thread_id, t.created_at
FROM threads t JOIN agents a ON a.active_thread_id = t.id AND a.id = ?
WHERE t.agent_id = a.id;

-- name: GetActiveThreadID :one
-- The thread the sink resolves when writing and replay resolves when
-- reading for the next executing prompt. sql.ErrNoRows means unknown
-- agent; this never repairs or creates.
SELECT active_thread_id FROM agents WHERE id = ?;

-- name: UpdateActiveThread :execrows
-- Pointer swap; callers serialize at settled turn boundaries. Zero
-- rows affected means the agent row does not exist.
UPDATE agents SET active_thread_id = ? WHERE id = ?;

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
-- Tool preferences live in their own rows, replaced by the same
-- transaction when the patch carries them.
UPDATE agents
SET name = ?, icon = ?, soul_personality = ?, soul_identity = ?,
    user_context = ?, operating_instructions = ?, tools_context = ?
WHERE id = ?;
