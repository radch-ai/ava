-- One row per message in an agent's conversation; tool rows carry
-- one completed invocation (the ToolEnvelope JSON) as their content,
-- error rows a failed turn's message. Only the gateway's
-- conversation turns write here; scheduled runs live in
-- agent_activity.
--
-- Working-context structure: turn_id groups a queue item's rows (the
-- stream id); tool_calls carries an assistant row's model-authored
-- call group as JSON [{"id","name","arguments"}]. The one raw read
-- walks (agent_id, created_at, id), which the page index below
-- already covers.
CREATE TABLE IF NOT EXISTS thread_messages (
    id         TEXT NOT NULL PRIMARY KEY,
    agent_id   TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'tool', 'error')),
    content    TEXT NOT NULL,
    created_at BIGINT NOT NULL
        DEFAULT (CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER)),
    turn_id    TEXT NOT NULL,
    tool_calls TEXT
);

-- Covers ListThreadRows' (agent_id, created_at ASC, id ASC) ordering
-- via reverse scan of the DESC index.
CREATE INDEX IF NOT EXISTS idx_thread_messages_page
    ON thread_messages (agent_id, created_at DESC, id DESC);
