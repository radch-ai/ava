-- name: ListProviders :many
-- Providers in creation order; api_key never leaves the backend.
-- models is the custom-id JSON array; system models are not DB state.
SELECT id, name, base_url, api_key, models
FROM providers
ORDER BY created_at ASC, id ASC;

-- name: GetProvider :one
-- Row for one provider; sql.ErrNoRows = unknown id.
SELECT id, name, base_url, api_key, models
FROM providers WHERE id = ?;

-- name: CreateProvider :exec
-- id and created_at are stamped by the caller/database.
INSERT INTO providers (id, name, base_url, api_key, models)
VALUES (?, ?, ?, ?, ?);

-- name: UpdateProvider :exec
-- Full-row write (models array included) in one atomic statement;
-- the handler applies absent-keeps-patch semantics.
UPDATE providers
SET name = ?, base_url = ?, api_key = ?, models = ?
WHERE id = ?;

-- name: DeleteProvider :exec
-- The custom model array dies with the row.
DELETE FROM providers WHERE id = ?;
