-- Drop indexes
DROP INDEX IF EXISTS idx_agent_tools_agent;
DROP INDEX IF EXISTS idx_cron_jobs_agent;

-- Drop tables in reverse dependency order
DROP TABLE IF EXISTS agent_tools;
DROP TABLE IF EXISTS cron_jobs;
DROP TABLE IF EXISTS heartbeat;
