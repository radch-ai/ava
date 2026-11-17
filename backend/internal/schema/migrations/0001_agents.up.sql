-- Agents: identity (name, icon) and the five prompt-text fields,
-- one typed column each; user_context is the superagent's, NULL on
-- regular agents. Heartbeat and cron jobs live in their own
-- agent-keyed tables, tool permissions in agent_tool_preferences.
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

-- The installed-tool catalog with its global switch. Built-ins ship
-- with the app and default ON; entries are synced from the gateway's
-- registry at boot, which never overwrites a saved switch.
CREATE TABLE IF NOT EXISTS tools (
    id          TEXT NOT NULL PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    -- 1 = shipped with the app (default-ON); 0 = user-installed.
    builtin     INTEGER NOT NULL DEFAULT 1,
    -- Global availability: the outer permission gate.
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  BIGINT NOT NULL
        DEFAULT (CAST((julianday('now') - 2440587.5) * 86400000000 AS INTEGER))
);

-- Per-agent tool preferences: a row means the tool is enabled for
-- that agent -- presence is the whole meaning, no per-agent switch.
-- The global switch lives on tools.enabled alone.
CREATE TABLE IF NOT EXISTS agent_tool_preferences (
    agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    tool_id  TEXT NOT NULL REFERENCES tools(id) ON DELETE CASCADE,
    PRIMARY KEY (agent_id, tool_id)
);

-- Built-ins default ON, as two insert rules: a new built-in reaches
-- every existing agent, a new agent reaches every installed built-in
-- -- globally disabled ones included, so a saved choice is
-- independent of availability. Only genuine inserts seed grants:
-- catalog re-sync takes the upsert's update path and toggles are
-- updates, so a deleted grant (the opt-out) never comes back.
-- A failed grant fails its owning insertion -- no half-initialized
-- defaults.
CREATE TRIGGER IF NOT EXISTS tools_seed_agent_grants
AFTER INSERT ON tools
FOR EACH ROW
WHEN NEW.builtin = 1
BEGIN
    INSERT INTO agent_tool_preferences (agent_id, tool_id)
    SELECT id, NEW.id FROM agents;
END;

CREATE TRIGGER IF NOT EXISTS agents_seed_tool_grants
AFTER INSERT ON agents
BEGIN
    INSERT INTO agent_tool_preferences (agent_id, tool_id)
    SELECT NEW.id, id FROM tools WHERE builtin = 1;
END;
