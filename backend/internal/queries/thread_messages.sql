-- Thread display and model-context reads, reference-aware.
--
-- A mid-loop rotation stores its retained tail as ordered references
-- (thread_context_refs) into the successor thread instead of moving
-- source rows: one source row has exactly one display location -- the
-- latest thread on the agent's active chain that carries it, or its
-- source thread when no successor carries it. Every query below
-- resolves that placement from the committed state alone.
--
-- The shared shape: `chain` walks the active thread's predecessor
-- lineage (depth = steps back from the active thread, so a smaller
-- depth is a later thread). A row the target would show -- its own, or
-- one its references carry -- is suppressed when a chain thread nearer
-- the head carries a reference to the same source row: the later
-- carrier displays it. The sort key is (sort_ms, band, sort_seq): the
-- seeded summary sorts first (its created_at, band 0), references
-- follow in position order (the seed's timestamp, band 1, zero-padded
-- position), and the thread's own new rows sort chronologically after
-- them (band 2) -- placement order, never source timestamps.
--
-- Display queries keep the visibility predicate (role != 'assistant'
-- OR content != ''): structure-only call rows never consume a page
-- slot. Both the display read and ListActiveThreadRows -- the model
-- replay scope -- return is_summary: the wire marks a compaction
-- successor's seeded summary row, and the gateway tells it from an
-- ordinary assistant row.
--
-- sqlc's analyzer resolves the recursive CTE only from the statement's
-- top level, so the target's chain depth joins in as a second `chain`
-- alias (tc/tc2) instead of a nested subquery; the keyset walks the
-- sort triple as an explicit disjunction for the same reason.

-- name: ListThreadPageRows :many
-- One thread's visible display rows, newest-first window starting at
-- its newest row. Empty assistant rows (structure-only) are excluded
-- here, before the limit: they never consume a page slot.
WITH RECURSIVE chain(thread_id, depth) AS (
    SELECT a.active_thread_id AS thread_id, 0 AS depth FROM agents a WHERE a.id = ?
    UNION ALL
    SELECT t.previous_thread_id AS thread_id, chain.depth + 1 AS depth
    FROM threads t JOIN chain ON t.id = chain.thread_id
    WHERE t.previous_thread_id IS NOT NULL
)
SELECT id, role, content, created_at, is_summary FROM (
    SELECT m.created_at AS sort_ms,
           CASE WHEN m.is_summary = 1 THEN 0 ELSE 2 END AS band,
           CASE WHEN m.is_summary = 1 THEN '' ELSE m.id END AS sort_seq,
           m.id AS id, m.role AS role, m.content AS content, m.created_at AS created_at,
           m.is_summary AS is_summary
    FROM thread_messages m
    WHERE m.agent_id = sqlc.arg(agent_id) AND m.thread_id = sqlc.arg(thread_id)
      AND (m.role != 'assistant' OR m.content != '')
      AND NOT EXISTS (
          SELECT 1 FROM thread_context_refs r
          JOIN chain c ON c.thread_id = r.thread_id
          JOIN chain tc ON tc.thread_id = m.thread_id
          WHERE r.message_id = m.id AND r.agent_id = sqlc.arg(agent_id) AND c.depth < tc.depth)
    UNION ALL
    SELECT (SELECT s.created_at FROM thread_messages s
             WHERE s.agent_id = sqlc.arg(agent_id) AND s.thread_id = sqlc.arg(thread_id)
               AND s.is_summary = 1
             ORDER BY s.created_at, s.id LIMIT 1),
           1, printf('%020d', r.position),
           src.id, src.role, src.content, src.created_at, src.is_summary
    FROM thread_context_refs r
    JOIN thread_messages src ON src.agent_id = r.agent_id AND src.id = r.message_id
    WHERE r.agent_id = sqlc.arg(agent_id) AND r.thread_id = sqlc.arg(thread_id)
      AND (src.role != 'assistant' OR src.content != '')
      AND NOT EXISTS (
          SELECT 1 FROM thread_context_refs r2
          JOIN chain c2 ON c2.thread_id = r2.thread_id
          JOIN chain tc2 ON tc2.thread_id = r.thread_id
          WHERE r2.message_id = src.id AND r2.agent_id = sqlc.arg(agent_id) AND c2.depth < tc2.depth)
)
ORDER BY sort_ms DESC, band DESC, sort_seq DESC
LIMIT sqlc.arg(row_limit);

-- name: ListThreadPageRowsBefore :many
-- The same page strictly older than an anchor row's placement key
-- (keyset continuation on the sort triple), so paging walks the union
-- -- own rows and references alike -- in display order.
WITH RECURSIVE chain(thread_id, depth) AS (
    SELECT a.active_thread_id AS thread_id, 0 AS depth FROM agents a WHERE a.id = ?
    UNION ALL
    SELECT t.previous_thread_id AS thread_id, chain.depth + 1 AS depth
    FROM threads t JOIN chain ON t.id = chain.thread_id
    WHERE t.previous_thread_id IS NOT NULL
)
SELECT id, role, content, created_at, is_summary FROM (
    SELECT m.created_at AS sort_ms,
           CASE WHEN m.is_summary = 1 THEN 0 ELSE 2 END AS band,
           CASE WHEN m.is_summary = 1 THEN '' ELSE m.id END AS sort_seq,
           m.id AS id, m.role AS role, m.content AS content, m.created_at AS created_at,
           m.is_summary AS is_summary
    FROM thread_messages m
    WHERE m.agent_id = sqlc.arg(agent_id) AND m.thread_id = sqlc.arg(thread_id)
      AND (m.role != 'assistant' OR m.content != '')
      AND NOT EXISTS (
          SELECT 1 FROM thread_context_refs r
          JOIN chain c ON c.thread_id = r.thread_id
          JOIN chain tc ON tc.thread_id = m.thread_id
          WHERE r.message_id = m.id AND r.agent_id = sqlc.arg(agent_id) AND c.depth < tc.depth)
    UNION ALL
    SELECT (SELECT s.created_at FROM thread_messages s
             WHERE s.agent_id = sqlc.arg(agent_id) AND s.thread_id = sqlc.arg(thread_id)
               AND s.is_summary = 1
             ORDER BY s.created_at, s.id LIMIT 1),
           1, printf('%020d', r.position),
           src.id, src.role, src.content, src.created_at, src.is_summary
    FROM thread_context_refs r
    JOIN thread_messages src ON src.agent_id = r.agent_id AND src.id = r.message_id
    WHERE r.agent_id = sqlc.arg(agent_id) AND r.thread_id = sqlc.arg(thread_id)
      AND (src.role != 'assistant' OR src.content != '')
      AND NOT EXISTS (
          SELECT 1 FROM thread_context_refs r2
          JOIN chain c2 ON c2.thread_id = r2.thread_id
          JOIN chain tc2 ON tc2.thread_id = r.thread_id
          WHERE r2.message_id = src.id AND r2.agent_id = sqlc.arg(agent_id) AND c2.depth < tc2.depth)
)
WHERE sort_ms < CAST(sqlc.arg(before_created_at) AS INTEGER)
   OR (sort_ms = sqlc.arg(before_created_at) AND band < CAST(sqlc.arg(before_band) AS INTEGER))
   OR (sort_ms = sqlc.arg(before_created_at) AND band = sqlc.arg(before_band)
       AND sort_seq < CAST(sqlc.arg(before_id) AS TEXT))
ORDER BY sort_ms DESC, band DESC, sort_seq DESC
LIMIT sqlc.arg(row_limit);

-- name: GetThreadMessageMeta :one
-- Cursor anchor validation: the message must be a row the target
-- thread displays -- physically stored there or carried by one of its
-- references. Returns the placement key the keyset read binds.
-- sql.ErrNoRows means invalid cursor.
WITH RECURSIVE chain(thread_id, depth) AS (
    SELECT a.active_thread_id AS thread_id, 0 AS depth FROM agents a WHERE a.id = ?
    UNION ALL
    SELECT t.previous_thread_id AS thread_id, chain.depth + 1 AS depth
    FROM threads t JOIN chain ON t.id = chain.thread_id
    WHERE t.previous_thread_id IS NOT NULL
)
SELECT sort_ms AS created_at, band, CAST(sort_seq AS TEXT) AS id FROM (
    SELECT m.created_at AS sort_ms,
           CASE WHEN m.is_summary = 1 THEN 0 ELSE 2 END AS band,
           CASE WHEN m.is_summary = 1 THEN '' ELSE m.id END AS sort_seq,
           m.id AS id, m.role AS role, m.content AS content, m.created_at AS created_at
    FROM thread_messages m
    WHERE m.agent_id = sqlc.arg(agent_id) AND m.thread_id = sqlc.arg(thread_id)
      AND (m.role != 'assistant' OR m.content != '')
      AND NOT EXISTS (
          SELECT 1 FROM thread_context_refs r
          JOIN chain c ON c.thread_id = r.thread_id
          JOIN chain tc ON tc.thread_id = m.thread_id
          WHERE r.message_id = m.id AND r.agent_id = sqlc.arg(agent_id) AND c.depth < tc.depth)
    AND m.id = sqlc.arg(anchor_id)
    UNION ALL
    SELECT (SELECT s.created_at FROM thread_messages s
             WHERE s.agent_id = sqlc.arg(agent_id) AND s.thread_id = sqlc.arg(thread_id)
               AND s.is_summary = 1
             ORDER BY s.created_at, s.id LIMIT 1),
           1, printf('%020d', r.position),
           src.id, src.role, src.content, src.created_at
    FROM thread_context_refs r
    JOIN thread_messages src ON src.agent_id = r.agent_id AND src.id = r.message_id
    WHERE r.agent_id = sqlc.arg(agent_id) AND r.thread_id = sqlc.arg(thread_id)
      AND (src.role != 'assistant' OR src.content != '')
      AND NOT EXISTS (
          SELECT 1 FROM thread_context_refs r2
          JOIN chain c2 ON c2.thread_id = r2.thread_id
          JOIN chain tc2 ON tc2.thread_id = r.thread_id
          WHERE r2.message_id = src.id AND r2.agent_id = sqlc.arg(agent_id) AND c2.depth < tc2.depth)
) placed
WHERE placed.id = sqlc.arg(anchor_id);

-- name: ListActiveThreadRows :many
-- The given thread's rows -- model replay scope, fed the agent's
-- active pointer by the caller. Unfiltered: the model sees structure
-- rows and every role; placement follows the same reference rules as
-- the display projection, with the seeded summary first.
WITH RECURSIVE chain(thread_id, depth) AS (
    SELECT a.active_thread_id AS thread_id, 0 AS depth FROM agents a WHERE a.id = ?
    UNION ALL
    SELECT t.previous_thread_id AS thread_id, chain.depth + 1 AS depth
    FROM threads t JOIN chain ON t.id = chain.thread_id
    WHERE t.previous_thread_id IS NOT NULL
)
SELECT id, role, content, created_at, turn_id, tool_calls, elided, is_summary FROM (
    SELECT m.created_at AS sort_ms,
           CASE WHEN m.is_summary = 1 THEN 0 ELSE 2 END AS band,
           CASE WHEN m.is_summary = 1 THEN '' ELSE m.id END AS sort_seq,
           m.id AS id, m.role AS role, m.content AS content, m.created_at AS created_at,
           m.turn_id AS turn_id, m.tool_calls AS tool_calls,
           m.elided AS elided, m.is_summary AS is_summary
    FROM thread_messages m
    WHERE m.agent_id = sqlc.arg(agent_id) AND m.thread_id = sqlc.arg(thread_id)
      AND NOT EXISTS (
          SELECT 1 FROM thread_context_refs r
          JOIN chain c ON c.thread_id = r.thread_id
          JOIN chain tc ON tc.thread_id = m.thread_id
          WHERE r.message_id = m.id AND r.agent_id = sqlc.arg(agent_id) AND c.depth < tc.depth)
    UNION ALL
    SELECT (SELECT s.created_at FROM thread_messages s
             WHERE s.agent_id = sqlc.arg(agent_id) AND s.thread_id = sqlc.arg(thread_id)
               AND s.is_summary = 1
             ORDER BY s.created_at, s.id LIMIT 1),
           1, printf('%020d', r.position),
           src.id, src.role, src.content, src.created_at,
           src.turn_id, src.tool_calls, src.elided, src.is_summary
    FROM thread_context_refs r
    JOIN thread_messages src ON src.agent_id = r.agent_id AND src.id = r.message_id
    WHERE r.agent_id = sqlc.arg(agent_id) AND r.thread_id = sqlc.arg(thread_id)
      AND NOT EXISTS (
          SELECT 1 FROM thread_context_refs r2
          JOIN chain c2 ON c2.thread_id = r2.thread_id
          JOIN chain tc2 ON tc2.thread_id = r.thread_id
          WHERE r2.message_id = src.id AND r2.agent_id = sqlc.arg(agent_id) AND c2.depth < tc2.depth)
)
ORDER BY sort_ms ASC, band ASC, sort_seq ASC;

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
-- thread_id is the active thread the sink resolves at write time;
-- is_summary marks a compaction successor's seeded summary row.
INSERT INTO thread_messages (id, agent_id, role, content, created_at, turn_id, tool_calls, thread_id, is_summary)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);
