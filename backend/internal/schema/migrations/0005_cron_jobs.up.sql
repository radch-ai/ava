-- Cron jobs keyed by agent. model is a plain string; NULL = default
-- routing ('auto') and staleness is caught at call time.
CREATE TABLE IF NOT EXISTS cron_jobs (
    id              TEXT NOT NULL PRIMARY KEY,
    agent_id        TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    time            TEXT NOT NULL,
    frequency       TEXT NOT NULL,
    cron_expression TEXT NULL DEFAULT NULL,
    model           TEXT NULL DEFAULT NULL,
    prompt          TEXT NOT NULL,
    created_at      BIGINT NOT NULL
        DEFAULT (CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER))
);
