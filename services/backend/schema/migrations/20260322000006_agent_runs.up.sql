CREATE TABLE IF NOT EXISTS agent_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id BIGINT NOT NULL,
    agent_name TEXT NOT NULL,
    triggered_by TEXT NOT NULL,
    action TEXT NOT NULL,
    status TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (user_id, agent_name) REFERENCES agents(user_id, name) ON DELETE CASCADE,
    CHECK (status IN ('running', 'completed', 'failed'))
);

CREATE INDEX IF NOT EXISTS idx_agent_runs_agent_created
    ON agent_runs(user_id, agent_name, created_at ASC, id ASC);
