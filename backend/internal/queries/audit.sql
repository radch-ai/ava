-- Durable, payload-free security audit. Every statement here
-- writes or reads the fixed column set of tool_audit_events: no query
-- accepts a free-form payload, so no caller can smuggle one in.

-- name: InsertAuditEvent :one
-- The pre-dispatch admission record (or a point-in-time decision). Its
-- returned id is the handle the outcome update closes.
INSERT INTO tool_audit_events (occurred_at, event, agent_id, server_id, tool_id,
                               tool_call_id, decision, reason_code,
                               count, state)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: UpdateAuditOutcome :execrows
-- Close one admission record: the observed outcome, terminal state.
UPDATE tool_audit_events
SET decision = ?, reason_code = ?, count = ?, state = 'terminal'
WHERE id = ?;

-- name: DeleteAuditOlderThan :execrows
-- Retention bound one, enforced: completed records past the cutoff go.
-- Admitted-until-terminal rows are protected.
DELETE FROM tool_audit_events WHERE state = 'terminal' AND occurred_at < ?;

-- name: DeleteAuditOverflow :execrows
-- Retention bound two: keep the newest completed records, drop the rest
-- of the terminal rows. Admitted rows never count against the cap. The
-- boundary row's id (OFFSET keep from the top) names everything older;
-- no rows means nothing to delete.
DELETE FROM tool_audit_events
WHERE state = 'terminal'
  AND id <= COALESCE((
    SELECT id FROM tool_audit_events WHERE state = 'terminal'
    ORDER BY id DESC LIMIT 1 OFFSET CAST(sqlc.arg('keep_count') AS INTEGER)
  ), 0);

-- name: CloseOrphanAdmissions :execrows
-- Startup reconciliation: an 'admitted' row that survived a restart has
-- an unknown remote outcome. It is closed truthfully, never replayed.
UPDATE tool_audit_events
SET decision = 'error', reason_code = 'restart_unknown', state = 'terminal'
WHERE state = 'admitted';
