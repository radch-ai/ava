-- One row per message in a thread; tool rows carry one
-- completed invocation (the ToolEnvelope JSON) as their content,
-- error rows a failed turn's message. Only the gateway's conversation
-- turns write here; scheduled runs live in agent_activity.
--
-- Working-context structure: turn_id groups a queue item's rows (the
-- stream id); tool_calls carries an assistant row's model-authored
-- call group as JSON [{"id","name","arguments"}]. Replay reads one
-- thread (agent_id, thread_id, created_at, id) via the thread index;
-- display walks the agent page index below.
CREATE TABLE IF NOT EXISTS thread_messages (
    id         TEXT NOT NULL PRIMARY KEY,
    agent_id   TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'tool', 'error')),
    content    TEXT NOT NULL,
    created_at BIGINT NOT NULL
        DEFAULT (CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER)),
    turn_id    TEXT NOT NULL,
    tool_calls TEXT,
    thread_id  TEXT NOT NULL,
    -- /shake's working-context elision flag: 1 means the model replay
    -- projection replaces this row's payload with a placeholder naming
    -- it. The stored content is never rewritten.
    elided     INTEGER NOT NULL DEFAULT 0 CHECK (elided IN (0, 1)),
    -- Composite ownership: a row can never name another agent's
    -- thread. Cascade on agent deletion, like the direct FK above.
    FOREIGN KEY (agent_id, thread_id) REFERENCES threads(agent_id, id)
        ON DELETE CASCADE
);

-- Covers ListActiveThreadRows' exact predicate and ordering — also
-- the display page read (same columns, keyset on created_at/id).
CREATE INDEX IF NOT EXISTS idx_thread_messages_thread
    ON thread_messages (agent_id, thread_id, created_at, id);
