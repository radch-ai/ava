-- queries/agent_config.sql
-- SQL queries for agent configuration operations
--
-- Schema Notes:
--   - Agents table: (user_id, name) composite PK, no UUID
--   - FK tables use agent_name column, referencing agents(user_id, name)
--   - Duplicate names auto-suffixed: name-2, name-3, etc.

-- CheckAgentNameAvailability checks if a name exists and returns all names with the same prefix.
-- Used to determine if the name can be used directly or needs a suffix.
-- name: CheckAgentNameAvailability :many
SELECT name
FROM agents
WHERE user_id = $1 AND (name = $2 OR name LIKE $2 || '-%')
ORDER BY name;

-- CreateAgent creates a new agent with the given configuration.
-- name: CreateAgent :exec
INSERT INTO agents (user_id, name, icon, config)
VALUES ($1, $2, $3, $4::jsonb);

-- GetAgentConfiguration retrieves the full configuration for an agent.
-- Aggregates data from agents, agent_tools, heartbeat, and cron_jobs tables.
-- name: GetAgentConfiguration :one
SELECT
    a.name,
    a.icon,
    a.config->'soul' as soul,
    a.config->>'userContext' as user_context,
    a.config->>'operatingInstructions' as operating_instructions,
    a.config->>'toolsContext' as tools_context,
    COALESCE(
        (SELECT jsonb_agg(t.id::text) FROM agent_tools at JOIN user_tools t ON t.id = at.tool_id WHERE at.user_id = a.user_id AND at.agent_name = a.name),
        '[]'::jsonb
    ) as tools_enabled,
    (SELECT jsonb_build_object(
        'model', h.model,
        'enabled', h.enabled,
        'duration', h.duration,
        'content', h.content
    ) FROM heartbeat h WHERE h.user_id = a.user_id AND h.agent_name = a.name) as heartbeat,
    (SELECT jsonb_agg(jsonb_build_object(
        'id', c.id::text,
        'time', c.time,
        'frequency', c.frequency,
        'cronExpression', c.cron_expression,
        'prompt', c.prompt
    )) FROM cron_jobs c WHERE c.user_id = a.user_id AND c.agent_name = a.name) as cron_jobs
FROM agents a
WHERE a.user_id = $1 AND a.name = $2;

-- UpdateAgentConfig merges new config values into the existing JSONB config.
-- name: UpdateAgentConfig :exec
UPDATE agents SET config = config || $1::jsonb, updated_at = NOW()
WHERE user_id = $2 AND name = $3;

-- UpdateAgentIcon updates the icon field for an agent.
-- name: UpdateAgentIcon :exec
UPDATE agents SET icon = $1, updated_at = NOW()
WHERE user_id = $2 AND name = $3;

-- ClearAgentTools removes all tool associations for an agent (does NOT delete the tools themselves).
-- name: ClearAgentTools :exec
DELETE FROM agent_tools WHERE user_id = $1 AND agent_name = $2;

-- InsertAgentTool associates a tool with an agent.
-- name: InsertAgentTool :exec
INSERT INTO agent_tools (user_id, agent_name, tool_id) VALUES ($1, $2, $3::uuid)
ON CONFLICT DO NOTHING;

-- UpsertHeartbeat inserts or updates the heartbeat config for an agent.
-- name: UpsertHeartbeat :exec
INSERT INTO heartbeat (user_id, agent_name, model, enabled, duration, content)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (user_id, agent_name) DO UPDATE SET
    model = EXCLUDED.model,
    enabled = EXCLUDED.enabled,
    duration = EXCLUDED.duration,
    content = EXCLUDED.content;

-- DeleteCronJobs removes all cron jobs for an agent.
-- name: DeleteCronJobs :exec
DELETE FROM cron_jobs WHERE user_id = $1 AND agent_name = $2;

-- InsertCronJob adds a new cron job for an agent.
-- name: InsertCronJob :exec
INSERT INTO cron_jobs (id, user_id, agent_name, time, frequency, cron_expression, prompt)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6);
