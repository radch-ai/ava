-- Agent configuration tables
--
-- Contains:
--   - heartbeat: 1:1 with agent (periodic check-in config)
--   - cron_jobs: 1:many with agent (scheduled tasks)
--   - agent_tools: junction table for agent-tool associations

-- Heartbeat table (1:1 with agent)
-- FK (user_id, agent_name) -> agents(user_id, name) also serves as PK.
CREATE TABLE IF NOT EXISTS heartbeat (
    user_id    UUID NOT NULL,
    agent_name TEXT NOT NULL,
    model      TEXT NOT NULL DEFAULT '',
    enabled    BOOLEAN DEFAULT false,
    duration   TEXT NOT NULL DEFAULT '1hr',
    content    TEXT NOT NULL DEFAULT '',
    FOREIGN KEY (user_id, agent_name) REFERENCES agents(user_id, name) ON DELETE CASCADE
);

-- Cron jobs table (1:many with agent)
-- Stores scheduled tasks for agents.
CREATE TABLE IF NOT EXISTS cron_jobs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL,
    agent_name      TEXT NOT NULL,
    time            TEXT NOT NULL DEFAULT '',
    frequency       TEXT NOT NULL DEFAULT 'daily',
    cron_expression TEXT,
    prompt          TEXT NOT NULL DEFAULT '',
    FOREIGN KEY (user_id, agent_name) REFERENCES agents(user_id, name) ON DELETE CASCADE
);

-- Agent tools junction table
-- Many-to-many relationship between agents and user tools.
CREATE TABLE IF NOT EXISTS agent_tools (
    user_id    UUID NOT NULL,
    agent_name TEXT NOT NULL,
    tool_id    UUID NOT NULL,
    PRIMARY KEY (user_id, agent_name, tool_id),
    FOREIGN KEY (user_id, agent_name) REFERENCES agents(user_id, name) ON DELETE CASCADE,
    FOREIGN KEY (tool_id) REFERENCES user_tools(id) ON DELETE CASCADE
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_cron_jobs_agent ON cron_jobs(user_id, agent_name);
CREATE INDEX IF NOT EXISTS idx_agent_tools_agent ON agent_tools(user_id, agent_name);
