-- Drop indexes
DROP INDEX IF EXISTS idx_user_tools_user_id;
DROP INDEX IF EXISTS idx_agent_tools_agent_id;
DROP INDEX IF EXISTS idx_cron_jobs_agent_id;
DROP INDEX IF EXISTS idx_agents_updated_at;
DROP INDEX IF EXISTS idx_agents_user_id;

-- Drop tables in reverse dependency order
DROP TABLE IF EXISTS agent_tools;
DROP TABLE IF EXISTS user_tools;
DROP TABLE IF EXISTS cron_jobs;
DROP TABLE IF EXISTS heartbeat;
DROP TABLE IF EXISTS agents;
DROP TABLE IF EXISTS users;
