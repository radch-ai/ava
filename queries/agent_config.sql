-- queries/agent_config.sql
-- SQL queries for agent configuration operations

-- name: GetAgentIDsByUserID :many
SELECT agent_id
FROM agents
WHERE user_id = $1;

-- name: GetAgentConfiguration :one
SELECT
    a.config->'soul' as soul,
    a.config->>'userContext' as user_context,
    a.config->>'operatingInstructions' as operating_instructions,
    a.config->>'toolsContext' as tools_context,
    COALESCE(
        (SELECT jsonb_agg(t.id::text) FROM agent_tools at JOIN user_tools t ON t.id = at.tool_id WHERE at.agent_id = a.agent_id),
        '[]'::jsonb
    ) as tools_enabled,
    (SELECT jsonb_build_object(
        'model', h.model,
        'enabled', h.enabled,
        'duration', h.duration,
        'content', h.content
    ) FROM heartbeat h WHERE h.agent_id = a.agent_id) as heartbeat,
    (SELECT jsonb_agg(jsonb_build_object(
        'id', c.id::text,
        'time', c.time,
        'frequency', c.frequency,
        'cronExpression', c.cron_expression,
        'prompt', c.prompt
    )) FROM cron_jobs c WHERE c.agent_id = a.agent_id) as cron_jobs
FROM agents a
WHERE a.agent_id = $1;

-- name: UpdateAgentConfig :exec
UPDATE agents SET config = config || $1::jsonb, updated_at = NOW()
WHERE agent_id = $2;

-- name: DeleteAgentTools :exec
DELETE FROM agent_tools WHERE agent_id = $1;

-- name: InsertAgentTool :exec
INSERT INTO agent_tools (agent_id, tool_id) VALUES ($1, $2::uuid)
ON CONFLICT DO NOTHING;

-- name: UpsertHeartbeat :exec
INSERT INTO heartbeat (agent_id, model, enabled, duration, content)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (agent_id) DO UPDATE SET
    model = EXCLUDED.model,
    enabled = EXCLUDED.enabled,
    duration = EXCLUDED.duration,
    content = EXCLUDED.content;

-- name: DeleteCronJobs :exec
DELETE FROM cron_jobs WHERE agent_id = $1;

-- name: InsertCronJob :exec
INSERT INTO cron_jobs (id, agent_id, time, frequency, cron_expression, prompt)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5);
