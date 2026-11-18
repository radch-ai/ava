-- name: MaxThreadCreatedAt :one
-- The highest minted row timestamp: seeds the writer's watermark so
-- a restart never mints behind existing rows.
SELECT CAST(COALESCE(MAX(created_at), 0) AS INTEGER) AS max_created_at FROM thread_messages;
