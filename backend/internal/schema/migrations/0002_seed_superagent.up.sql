-- Seed the preconfigured superagent and guard system agents.
--
-- "_superagent" matches the UI's SUPERAGENT_AGENT constant
-- (ui/web/src/lib/types/agent.ts) — same name and icon. Its "_"
-- prefix is system-reserved and underivable from display names, so a
-- user-created agent can never collide with it.
--
-- The trigger rejects DELETEs of any "_"-prefixed id at the database
-- level: endpoints, scripts, and manual SQL all hit the same wall —
-- no code path can bypass the guard.
INSERT INTO agents (id, name, icon, created_at)
VALUES ('_superagent', 'Ava', 'message-square',
        CAST(strftime('%s', 'now') AS BIGINT) * 1000000)
ON CONFLICT DO NOTHING;

CREATE TRIGGER IF NOT EXISTS agents_no_delete_system
BEFORE DELETE ON agents
FOR EACH ROW
WHEN substr(OLD.id, 1, 1) = '_'
BEGIN
    SELECT RAISE(ABORT, 'system agents cannot be deleted');
END;
