-- name: ListCronJobs :many
-- The agent's cron jobs, oldest first; ties broken by id for a
-- stable order.
SELECT * FROM cron_jobs WHERE agent_id = ? ORDER BY created_at ASC, id ASC;

-- name: GetCronJob :one
-- One job scoped to its owning agent.
SELECT * FROM cron_jobs WHERE id = ? AND agent_id = ?;

-- name: CreateCronJob :exec
-- Store one job verbatim; created_at is DB-stamped.
INSERT INTO cron_jobs (id, agent_id, time, frequency, cron_expression, model, prompt)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: UpdateCronJob :exec
-- Full-row rewrite of the editable columns, scoped to the agent.
UPDATE cron_jobs
SET time = ?, frequency = ?, cron_expression = ?, model = ?, prompt = ?
WHERE id = ? AND agent_id = ?;

-- name: DeleteCronJob :execrows
-- Delete scoped to the owning agent.
DELETE FROM cron_jobs WHERE id = ? AND agent_id = ?;
