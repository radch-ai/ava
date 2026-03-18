-- queries/agents.sql
-- SQL queries for agent list operations
--
-- Schema Notes:
--   - Agents table: (user_id, name) composite PK, no UUID
--   - FK tables use agent_name column, referencing agents(user_id, name)

-- GetAgentsForUser returns all agents (name and icon) for a given user.
-- name: GetAgentsForUser :many
SELECT name, icon
FROM agents
WHERE user_id = $1
  AND name != 'lyla'
ORDER BY name;
