-- name: ListProviders :many
-- Providers in creation order; api_key never leaves the backend.
SELECT id, name, base_url, api_key
FROM providers
ORDER BY created_at ASC, id ASC;

-- name: ListModels :many
-- Every catalog row; grouping into providers happens in the handler.
SELECT model_id, provider_id
FROM models
ORDER BY provider_id ASC, model_id ASC;

-- name: ListProviderModels :many
-- One provider's model ids in order.
SELECT model_id FROM models WHERE provider_id = ? ORDER BY model_id ASC;

-- name: GetProvider :one
-- Row for one provider; sql.ErrNoRows = unknown id.
SELECT id, name, base_url, api_key
FROM providers WHERE id = ?;

-- name: CreateProvider :exec
-- id and created_at are stamped by the caller/database.
INSERT INTO providers (id, name, base_url, api_key)
VALUES (?, ?, ?, ?);

-- name: UpdateProvider :exec
-- Full-row write; the handler applies absent-keeps-patch semantics.
UPDATE providers
SET name = ?, base_url = ?, api_key = ?
WHERE id = ?;

-- name: DeleteProvider :exec
-- Cascades the provider's models away.
DELETE FROM providers WHERE id = ?;

-- name: DeleteProviderModels :exec
-- First half of the wholesale PATCH models replace.
DELETE FROM models WHERE provider_id = ?;

-- name: CreateModel :exec
-- Second half of the models replace; a UNIQUE (model_id) hit means
-- another provider owns the id.
INSERT INTO models (model_id, provider_id) VALUES (?, ?);
