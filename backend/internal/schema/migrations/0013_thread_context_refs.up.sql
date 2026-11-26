-- Mid-loop compaction's durable carryover: a seeded-summary marker and
-- the ordered reference table a rotation's retained tail stores as.
--   - is_summary marks a compaction thread's seeded summary row — the
--     identity range detection and the successor projection's
--     sort band key on. Backfilled onto every existing kind='compact'
--     thread's first row when it is an assistant row: the seed every
--     rotation this codebase ships writes first.
--   - thread_context_refs carries the explicitly retained segment of
--     one rotation: one row per original thread_messages row, in
--     native order. Sources are never moved or copied — position is
--     the successor's own ordering, unique per thread.
CREATE UNIQUE INDEX IF NOT EXISTS idx_thread_messages_agent_row
    ON thread_messages (agent_id, id);

ALTER TABLE thread_messages ADD COLUMN is_summary INTEGER NOT NULL DEFAULT 0 CHECK (is_summary IN (0,1));

UPDATE thread_messages SET is_summary = 1
 WHERE id IN (
   SELECT m.id FROM thread_messages m
   JOIN threads t ON t.agent_id = m.agent_id AND t.id = m.thread_id
   WHERE t.kind = 'compact' AND m.role = 'assistant'
     AND m.id = (
       SELECT m2.id FROM thread_messages m2
       WHERE m2.agent_id = m.agent_id AND m2.thread_id = m.thread_id
       ORDER BY m2.created_at, m2.id LIMIT 1));

CREATE TABLE thread_context_refs (
  agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  thread_id TEXT NOT NULL,
  position INTEGER NOT NULL CHECK (position >= 0),
  message_id TEXT NOT NULL,
  PRIMARY KEY (thread_id, position),
  UNIQUE (thread_id, message_id),
  FOREIGN KEY (agent_id, thread_id) REFERENCES threads(agent_id, id) ON DELETE CASCADE,
  FOREIGN KEY (agent_id, message_id) REFERENCES thread_messages(agent_id, id) ON DELETE CASCADE
);
CREATE INDEX idx_thread_context_refs_message ON thread_context_refs(agent_id, message_id);
