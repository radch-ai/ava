DROP TRIGGER IF EXISTS agents_no_delete_system;
DELETE FROM agents WHERE id = '_superagent';
