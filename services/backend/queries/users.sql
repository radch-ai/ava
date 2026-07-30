-- queries/users.sql
-- SQL queries for user lifecycle operations (webhooks)

-- CreateUser inserts a new user. Idempotent - safe if user already exists.
-- name: CreateUser :exec
INSERT INTO users (user_id)
VALUES ($1)
ON CONFLICT (user_id) DO NOTHING;

-- DeleteUser deletes a user. CASCADE removes all related data (agents, configs, tools).
-- name: DeleteUser :exec
DELETE FROM users
WHERE user_id = $1;
