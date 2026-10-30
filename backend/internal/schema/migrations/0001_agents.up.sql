-- Agents table: one row per agent, keyed by id.
--
-- Design:
--   - ids are derived from display names (lowercase-hyphen); "_"-prefixed
--     ids are system-reserved (e.g. "_superagent") and underivable from
--     user names
--   - timestamps are BIGINT micros since epoch
CREATE TABLE IF NOT EXISTS agents (
    id         TEXT NOT NULL PRIMARY KEY,
    name       TEXT NOT NULL,
    icon       TEXT NOT NULL,
    created_at BIGINT NOT NULL
);
