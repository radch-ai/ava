-- name: ListThreadPageRows :many
-- One thread's visible display rows, newest-first window starting at
-- its newest row. Empty assistant rows (structure-only) are excluded
-- here, before the limit: they never consume a page slot. Visible =
-- role != 'assistant' OR content != ''.
SELECT id, role, content, created_at
FROM thread_messages
WHERE agent_id = ? AND thread_id = ?
  AND (role != 'assistant' OR content != '')
ORDER BY created_at DESC, id DESC
LIMIT ?;

-- name: ListThreadPageRowsBefore :many
-- The same page strictly older than an anchor row of the same thread
-- (keyset continuation on (created_at, id)), covered by
-- idx_thread_messages_thread. The caller resolves the anchor row and
-- binds its (created_at, id) as a row value so sqlite can seek.
-- Visibility filters before the limit, exactly like the entry read.
SELECT id, role, content, created_at
FROM thread_messages
WHERE agent_id = sqlc.arg(agent_id) AND thread_id = sqlc.arg(thread_id)
  AND (created_at, id) < (CAST(sqlc.arg(before_created_at) AS INTEGER), CAST(sqlc.arg(before_id) AS TEXT))
  AND (role != 'assistant' OR content != '')
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(limit);

-- name: GetThreadMessageMeta :one
-- Cursor anchor validation: the message must be a visible row of
-- this thread. sql.ErrNoRows means invalid cursor.
SELECT created_at, id FROM thread_messages
WHERE agent_id = ? AND thread_id = ? AND id = ?
  AND (role != 'assistant' OR content != '');

-- name: ListActiveThreadRows :many
-- The given thread's rows -- model replay scope, fed the agent's
-- active pointer by the caller.
SELECT id, role, content, created_at, turn_id, tool_calls, elided
FROM thread_messages
WHERE agent_id = ? AND thread_id = ?
ORDER BY created_at ASC, id ASC;

-- name: GetThreadNewestTurn :one
-- /shake's protection anchor: the newest row's turn, whatever its
-- role -- the active exchange is the thread's newest turn, which may
-- carry no tool rows of its own. sql.ErrNoRows means the thread has
-- no rows, so there is nothing to protect or elide.
SELECT turn_id FROM thread_messages
WHERE agent_id = ? AND thread_id = ?
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: ListShakeEligibleToolRows :many
-- /shake's selection: every tool row of this thread that still carries
-- a payload worth replacing. octet_length counts bytes -- length()
-- counts characters and would drop multibyte payloads past the byte
-- floor -- and is only a prefilter: the envelope JSON is never shorter
-- than the result it wraps, so no elidable row is lost, but the exact
-- size check stays with the caller, which decodes the envelope.
SELECT id, turn_id, content FROM thread_messages
WHERE agent_id = ? AND thread_id = ? AND role = 'tool' AND elided = 0
  AND octet_length(content) >= CAST(sqlc.arg(min_bytes) AS INTEGER)
ORDER BY created_at ASC, id ASC;

-- name: MarkThreadRowElided :execrows
-- Flips one tool row to the elided placeholder. The elided = 0 predicate
-- makes the write idempotent: a repeat affects zero rows.
UPDATE thread_messages SET elided = 1
WHERE agent_id = ? AND thread_id = ? AND id = ? AND elided = 0 AND role = 'tool';

-- name: GetThreadToolRow :one
-- Retrieval read: one elided tool row's stored content, scoped to this
-- agent. sql.ErrNoRows covers every unusable reference -- unknown,
-- another agent's, or a row whose payload is already in context. No
-- thread predicate is deliberate: a reference carried into a successor
-- thread by compaction must keep resolving, and one agent's threads
-- share one principal.
SELECT content FROM thread_messages
WHERE agent_id = ? AND id = ? AND role = 'tool' AND elided = 1;

-- name: InsertThreadMessage :exec
-- One turn message; the caller mints the id and the ms timestamp.
-- turn_id groups the queue item's rows; tool_calls is the assistant
-- row's model-authored call group as JSON [{"id","name","arguments"}];
-- thread_id is the active thread the sink resolves at write time.
INSERT INTO thread_messages (id, agent_id, role, content, created_at, turn_id, tool_calls, thread_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);
