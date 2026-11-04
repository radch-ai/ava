-- One row per agent; deleting the agent removes it. model NULL =
-- follow the default routing.
CREATE TABLE IF NOT EXISTS heartbeat (
    agent_id TEXT NOT NULL PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
    model    TEXT NULL DEFAULT NULL REFERENCES models(id),
    enabled  INTEGER NOT NULL DEFAULT 0,
    duration TEXT NOT NULL DEFAULT '30m',
    content  TEXT NOT NULL DEFAULT ''
);
