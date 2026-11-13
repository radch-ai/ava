-- name: GetHeartbeat :one
-- The agent's single heartbeat row; sql.ErrNoRows means the agent
-- runs on the default heartbeat ({auto, disabled, 1hr, ""}).
SELECT * FROM heartbeat WHERE agent_id = ?;

-- name: UpdateHeartbeat :exec
-- One row per agent: the save endpoint writes the whole config.
INSERT INTO heartbeat (agent_id, model, provider_id, enabled, duration, content)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (agent_id) DO UPDATE
SET model = excluded.model,
    provider_id = excluded.provider_id,
    enabled = excluded.enabled,
    duration = excluded.duration,
    content = excluded.content;
