-- Agents: identity (name, icon) and the five prompt-text fields,
-- one typed column each; user_context is the superagent's, NULL on
-- regular agents. Heartbeat and cron jobs live in their own
-- agent-keyed tables.
--   - ids derive from display names (lowercase-hyphen); "_"-prefixed
--     ids are system-reserved and underivable from names
--   - timestamps are BIGINT micros since epoch, stamped by the
--     database on insert
CREATE TABLE IF NOT EXISTS agents (
    id                     TEXT NOT NULL PRIMARY KEY,
    name                   TEXT NOT NULL,
    icon                   TEXT NOT NULL,
    soul_personality       TEXT NOT NULL,
    soul_identity          TEXT NOT NULL,
    user_context           TEXT NULL,
    operating_instructions TEXT NOT NULL,
    tools_context          TEXT NOT NULL,
    created_at             BIGINT NOT NULL
        DEFAULT (CAST((julianday('now') - 2440587.5) * 86400000000 AS INTEGER))
);
