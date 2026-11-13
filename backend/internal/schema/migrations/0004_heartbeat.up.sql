-- One row per agent. The model pair (id + owning provider row) NULL
-- = default routing; deleting the provider nulls the owner.
CREATE TABLE IF NOT EXISTS heartbeat (
    agent_id    TEXT NOT NULL PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
    model       TEXT NULL DEFAULT NULL,
    provider_id TEXT NULL DEFAULT NULL REFERENCES providers(id) ON DELETE SET NULL,
    enabled     INTEGER NOT NULL DEFAULT 0,
    duration    TEXT NOT NULL DEFAULT '30m',
    content     TEXT NOT NULL DEFAULT ''
);
