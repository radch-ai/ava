-- name: GetDefaultModel :one
-- The single default row; sql.ErrNoRows = no default set.
SELECT model, provider_id
FROM default_model WHERE id = 1;
