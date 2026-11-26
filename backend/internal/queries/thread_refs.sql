-- Mid-loop compaction's durable carryover: ordered references from a
-- successor thread to the original rows its rotation retained, and the
-- reads that resolve them.

-- name: InsertThreadContextRef :exec
-- One ordered member of a successor's retained tail. position is the
-- successor's own ordering; the referenced row never moves or changes.
INSERT INTO thread_context_refs (agent_id, thread_id, position, message_id)
VALUES (?, ?, ?, ?);

-- name: ListThreadContextRefs :many
-- One thread's references in retained order -- source row ids only;
-- the row columns come from the projection's own join.
SELECT position, message_id FROM thread_context_refs
 WHERE agent_id = ? AND thread_id = ? ORDER BY position;

-- name: ListItemTurnRows :many
-- One item's physical rows in one thread, native order -- the retained
-- set a mid-turn rotation selects from. No placement filtering: this
-- is the source of truth about what the turn wrote and where.
SELECT id, role, content, created_at, turn_id, tool_calls, thread_id, elided, is_summary
  FROM thread_messages
 WHERE agent_id = ? AND thread_id = ? AND turn_id = ?
 ORDER BY created_at, id;
