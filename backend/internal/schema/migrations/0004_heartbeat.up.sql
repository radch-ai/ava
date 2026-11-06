-- One row per agent. model NULL = default routing ('auto'); the
-- column is a plain string — staleness is caught at call time, not
-- by the schema.
CREATE TABLE IF NOT EXISTS heartbeat (
    agent_id TEXT NOT NULL PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
    model    TEXT NULL DEFAULT NULL,
    enabled  INTEGER NOT NULL DEFAULT 0,
    duration TEXT NOT NULL DEFAULT '30m',
    content  TEXT NOT NULL DEFAULT ''
);
