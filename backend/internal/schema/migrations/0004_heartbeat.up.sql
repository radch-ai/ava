-- One row per agent. model NULL = default routing; a dropped model
-- sets it back to NULL.
CREATE TABLE IF NOT EXISTS heartbeat (
    agent_id TEXT NOT NULL PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
    model    TEXT NULL DEFAULT NULL REFERENCES models(model_id) ON DELETE SET NULL,
    enabled  INTEGER NOT NULL DEFAULT 0,
    duration TEXT NOT NULL DEFAULT '30m',
    content  TEXT NOT NULL DEFAULT ''
);
