-- Agents: one row per agent — AgentIdentity (name, icon) in columns,
-- the {} config field in config, an opaque JSON document.
--   - ids derive from display names (lowercase-hyphen); "_"-prefixed
--     ids are system-reserved and underivable from names
--   - timestamps are BIGINT micros since epoch, stamped by the
--     database on insert
CREATE TABLE IF NOT EXISTS agents (
    id         TEXT NOT NULL PRIMARY KEY,
    name       TEXT NOT NULL,
    icon       TEXT NOT NULL,
    config     TEXT NOT NULL, -- the {} config field
    created_at BIGINT NOT NULL
        DEFAULT (CAST((julianday('now') - 2440587.5) * 86400000000 AS INTEGER))
);
