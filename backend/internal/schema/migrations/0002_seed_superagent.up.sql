-- Seed the preconfigured superagent and guard system agents.
-- "_superagent" matches the UI's SUPERAGENT_AGENT_ID constant — same
-- name and icon; its config field mirrors the mock's SUPERAGENT_CONFIG
-- (ui/web/src/lib/mocks/data.ts). The "_" prefix is system-reserved
-- (0001), so user-created agents never collide; the trigger makes
-- system rows undeletable at the database level.
INSERT INTO agents (id, name, icon, config, created_at)
VALUES ('_superagent', 'Ava', 'message-square',
        '{"soul":{"personality":"Warm, direct, and precise. Prefers short answers with concrete next steps.","identity":"Ava is the user''s primary assistant and coordinates the other agents."},"userContext":"The user builds personal automation tooling and prefers concise, technical answers.","operatingInstructions":"Keep replies under five lines unless asked to elaborate. Surface agent failures immediately with a suggested fix.","toolsContext":"Local tools: file system read/write, browser, and shell access are available on the user''s devices.","toolsEnabled":["browser.browse","fs.read","fs.write","shell.run"],"cronJobs":[{"id":"cron-digest","time":"09:00","frequency":"daily","model":"gpt-4","prompt":"Compile the overnight digest: agent failures, research highlights, and today’s action items."}],"heartbeat":{"id":"heartbeat-ava","model":"auto","enabled":true,"duration":"30m","content":"Check agent heartbeats and flag anything stalled."}}',
        CAST(strftime('%s', 'now') AS BIGINT) * 1000000)
ON CONFLICT DO NOTHING;

CREATE TRIGGER IF NOT EXISTS agents_no_delete_system
BEFORE DELETE ON agents
FOR EACH ROW
WHEN substr(OLD.id, 1, 1) = '_'
BEGIN
    SELECT RAISE(ABORT, 'system agents cannot be deleted');
END;
