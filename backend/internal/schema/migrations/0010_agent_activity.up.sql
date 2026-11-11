-- One row per background execution (heartbeat, cron, automations):
-- the prompt lives on the config row; message carries the response
-- summary, NULL when there is nothing to say.
CREATE TABLE IF NOT EXISTS agent_activity (
    id           TEXT NOT NULL PRIMARY KEY,
    agent_id     TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    triggered_by TEXT NOT NULL CHECK (triggered_by IN ('heartbeat', 'cron')),
    action       TEXT NOT NULL,
    status       TEXT NOT NULL CHECK (status IN ('running', 'completed', 'failed')),
    message      TEXT NULL DEFAULT NULL,
    created_at   BIGINT NOT NULL
        DEFAULT (CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER))
);

-- Keyset page scan: newest first, id breaks ties for stable order.
CREATE INDEX IF NOT EXISTS idx_agent_activity_page
    ON agent_activity (agent_id, created_at DESC, id DESC);
