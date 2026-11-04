DELETE FROM heartbeat WHERE agent_id = '_superagent';
DROP TRIGGER IF EXISTS agents_no_delete_system;
DELETE FROM agents WHERE id = '_superagent';
