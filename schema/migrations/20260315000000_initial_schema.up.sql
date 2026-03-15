-- Users table
CREATE TABLE IF NOT EXISTS users (
    user_id         UUID PRIMARY KEY,
    subscription_vk TEXT,
    topup_vk        TEXT
);

-- Agents table
CREATE TABLE IF NOT EXISTS agents (
    agent_id     UUID PRIMARY KEY,
    user_id      UUID NOT NULL,
    agent_type   TEXT NOT NULL DEFAULT 'lyla',
    config       JSONB NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ DEFAULT NOW(),
    updated_at   TIMESTAMPTZ DEFAULT NOW(),
    FOREIGN KEY (user_id) REFERENCES users(user_id) ON DELETE CASCADE
);

-- Heartbeat table (1:1 with agent)
CREATE TABLE IF NOT EXISTS heartbeat (
    agent_id     UUID PRIMARY KEY REFERENCES agents(agent_id) ON DELETE CASCADE,
    model        TEXT NOT NULL DEFAULT '',
    enabled      BOOLEAN DEFAULT false,
    duration     TEXT NOT NULL DEFAULT '1hr',
    content      TEXT NOT NULL DEFAULT ''
);

-- Cron jobs table (1:many with agent)
CREATE TABLE IF NOT EXISTS cron_jobs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id        UUID NOT NULL,
    time            TEXT NOT NULL DEFAULT '',
    frequency       TEXT NOT NULL DEFAULT 'daily',
    cron_expression TEXT,
    prompt          TEXT NOT NULL DEFAULT '',
    FOREIGN KEY (agent_id) REFERENCES agents(agent_id) ON DELETE CASCADE
);

-- User tools table
CREATE TABLE IF NOT EXISTS user_tools (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL,
    name         TEXT NOT NULL,
    config       JSONB DEFAULT '{}',
    created_at   TIMESTAMPTZ DEFAULT NOW(),
    FOREIGN KEY (user_id) REFERENCES users(user_id) ON DELETE CASCADE
);

-- Agent tools junction table
CREATE TABLE IF NOT EXISTS agent_tools (
    agent_id UUID NOT NULL REFERENCES agents(agent_id) ON DELETE CASCADE,
    tool_id  UUID NOT NULL REFERENCES user_tools(id) ON DELETE CASCADE,
    PRIMARY KEY (agent_id, tool_id)
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_agents_user_id ON agents(user_id);
CREATE INDEX IF NOT EXISTS idx_agents_updated_at ON agents(updated_at);
CREATE INDEX IF NOT EXISTS idx_cron_jobs_agent_id ON cron_jobs(agent_id);
CREATE INDEX IF NOT EXISTS idx_agent_tools_agent_id ON agent_tools(agent_id);
CREATE INDEX IF NOT EXISTS idx_user_tools_user_id ON user_tools(user_id);
