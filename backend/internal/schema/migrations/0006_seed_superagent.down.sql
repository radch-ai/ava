DELETE FROM heartbeat WHERE agent_id = '_superagent';
DELETE FROM agents WHERE id = '_superagent';
-- Explicit for the FK-off migrate-CLI path; the agent delete above
-- already cascades under enforcement.
DELETE FROM threads WHERE id = '_superagent-thread-1';
