-- One row per message in an agent's conversation. Only the gateway's
-- conversation turns write here; scheduled runs live in agent_activity.
CREATE TABLE IF NOT EXISTS thread_messages (
    id         TEXT NOT NULL PRIMARY KEY,
    agent_id   TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
    content    TEXT NOT NULL,
    created_at BIGINT NOT NULL
        DEFAULT (CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER))
);

-- Keyset page scan: newest first, id breaks ties for stable order.
CREATE INDEX IF NOT EXISTS idx_thread_messages_page
    ON thread_messages (agent_id, created_at DESC, id DESC);
