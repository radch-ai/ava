-- Lifecycle journal: item bookkeeping, per-agent journal coordinates,
-- ordered event rows, and server-assigned tool invocation identities.
-- Every item/journal write happens inside one transaction with the
-- conversation rows it describes, so a rollback leaves neither.

-- name: GetSyncHistory :one
-- This database's lifecycle-history identity: mints a cursor's history
-- half. One row, created with the schema.
SELECT history_id FROM sync_history WHERE id = 1;

-- name: GetAgentSyncState :one
-- Journal coordinates: head is the newest committed position, floor the
-- oldest retained one. sql.ErrNoRows means no item was ever admitted.
SELECT head_pos, floor_pos FROM agent_sync_state WHERE agent_id = ?;

-- name: ReplaceAgentSyncHead :exec
-- Installs an agent's head inside the appending transaction: the first
-- append creates the coordinates row, later ones advance it. A position
-- is never reused, so this only ever moves forward.
INSERT INTO agent_sync_state (agent_id, head_pos) VALUES (?, ?)
ON CONFLICT(agent_id) DO UPDATE SET head_pos = excluded.head_pos;

-- name: SetAgentSyncFloor :exec
-- Advances the replay floor after retention deleted a prefix.
UPDATE agent_sync_state SET floor_pos = ? WHERE agent_id = ?;

-- name: InsertQueueItem :exec
-- One admitted item. model is the JSON selection reference (NULL =
-- default routing); accepted_ms is the admission timestamp.
INSERT INTO queue_items (id, agent_id, kind, content, model, state, accepted_ms)
VALUES (?, ?, ?, ?, ?, 'accepted', ?);

-- name: GetQueueItem :one
-- One item's bookkeeping row; sql.ErrNoRows means the agent never
-- admitted it.
SELECT * FROM queue_items WHERE agent_id = ? AND id = ?;

-- name: SetQueueItemStarted :execrows
-- accepted -> started. The state predicate in the WHERE clause is the
-- transition guard: zero rows means the item was not accepted (already
-- started or settled), which callers treat as a conflict.
UPDATE queue_items
SET state = 'started', thread_id = ?, user_message_id = ?, started_ms = ?
WHERE agent_id = ? AND id = ? AND state = 'accepted';

-- name: CancelAcceptedQueueItem :execrows
-- accepted -> cancelled: the only terminal transition before execution
-- starts. Restart recovery cancels admitted-but-never-started items
-- with it.
UPDATE queue_items
SET state = 'cancelled', finished_ms = ?, reason = ?
WHERE agent_id = ? AND id = ? AND state = 'accepted';

-- name: SetQueueItemTerminal :execrows
-- started -> one terminal outcome. The predicate makes a second terminal
-- write a no-op, so recovery and settlement can never contradict.
UPDATE queue_items
SET state = ?, finished_ms = ?, reason = ?
WHERE agent_id = ? AND id = ? AND state = 'started';

-- name: RebindQueueItemThread :execrows
-- Redirects one started item's subsequent writes to its mid-loop
-- rotation successor. The state and expected-source predicates make a
-- stale or racing rebind fail closed: zero rows means the item was not
-- running in the thread the caller believed active.
UPDATE queue_items SET thread_id = ?
 WHERE agent_id = ? AND id = ? AND state = 'started' AND thread_id = ?;

-- name: ListUnfinishedItems :many
-- Every item without a terminal outcome, admission order. Restart
-- recovery settles these; nothing re-executes them.
SELECT * FROM queue_items
WHERE state IN ('accepted', 'started')
ORDER BY accepted_ms ASC, id ASC;

-- name: DeleteOrphanTerminalItems :execrows
-- Terminal items no retained journal row references: their bookkeeping
-- is no longer needed. Unresolved items are never deleted here.
DELETE FROM queue_items
WHERE queue_items.agent_id = ? AND state NOT IN ('accepted', 'started')
  AND NOT EXISTS (
    SELECT 1 FROM lifecycle_journal j
    WHERE j.agent_id = queue_items.agent_id AND j.item_id = queue_items.id
  );

-- name: InsertToolInvocation :exec
-- One executing call, keyed by the server-minted invocation id. The
-- provider's call id is metadata: repeated tool rounds may reuse it.
INSERT INTO tool_invocations (id, agent_id, item_id, call_id, name, args, started_ms, state)
VALUES (?, ?, ?, ?, ?, ?, ?, 'open');

-- name: GetToolInvocation :one
-- The invocation's recorded intent: what was authorized to run.
SELECT * FROM tool_invocations WHERE agent_id = ? AND item_id = ? AND id = ?;

-- name: FinishToolInvocation :execrows
-- open -> finished. The predicate makes a repeat a no-op.
UPDATE tool_invocations SET state = 'finished'
WHERE agent_id = ? AND item_id = ? AND id = ? AND state = 'open';

-- name: InterruptOpenToolInvocations :execrows
-- Recovery's settlement for a started item's still-open invocations:
-- the external outcome is unknown, and 'finished' with no recorded
-- result says exactly that.
UPDATE tool_invocations SET state = 'finished'
WHERE agent_id = ? AND item_id = ? AND state = 'open';

-- name: InsertLifecycleEvent :exec
-- One ordered journal row. batch_end marks the last event of its
-- transaction, the only place replay and retention may cut; outcome
-- lifts an item_finished event's classification out of the payload for
-- the snapshot's cancelled-notice read.
INSERT INTO lifecycle_journal (agent_id, pos, item_id, kind, batch_end, payload, created_ms, outcome)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListLifecycleEventsAfter :many
-- Replay read: events committed after the cursor position, oldest first.
SELECT pos, payload, batch_end FROM lifecycle_journal
WHERE agent_id = ? AND pos > ?
ORDER BY pos ASC
LIMIT ?;


-- name: ListJournalAgents :many
-- Every agent with journal coordinates: the boot-time retention pass
-- prunes each of these after recovery settles unfinished items.
SELECT agent_id FROM agent_sync_state;

-- name: ListRecentCancelledEvents :many
-- Snapshot notices: the newest cancelled-item terminal events inside
-- the retained window. A cancellation mints no transcript row, so the
-- authoritative baseline carries these for a fresh projection.
SELECT payload FROM lifecycle_journal
WHERE lifecycle_journal.agent_id = ? AND outcome = 'cancelled'
  AND pos >= (SELECT s.floor_pos FROM agent_sync_state s WHERE s.agent_id = lifecycle_journal.agent_id)
ORDER BY pos DESC
LIMIT ?;


-- name: MaxLifecycleBatchEnd :one
-- Retention's cut: the last batch boundary at or below the target
-- position. 0 means no complete batch is old enough to delete.
SELECT CAST(COALESCE(MAX(pos), 0) AS INTEGER) AS pos FROM lifecycle_journal
WHERE agent_id = ? AND pos <= ? AND batch_end = 1;

-- name: DeleteLifecycleEventsUpTo :exec
-- Removes the retained prefix through a batch boundary.
DELETE FROM lifecycle_journal WHERE agent_id = ? AND pos <= ?;

-- name: CountOpenItemInvocations :one
-- The mid-loop rotation's settle guard: one checkpoint may rotate only
-- between rounds, when every dispatched invocation of the item has its
-- outcome recorded. Zero is the only admissible answer.
SELECT COUNT(*) FROM tool_invocations
WHERE agent_id = ? AND item_id = ? AND state = 'open';
