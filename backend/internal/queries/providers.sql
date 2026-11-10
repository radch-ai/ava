-- name: ListProviders :many
-- Providers in creation order. Provider keys never leave the vault:
-- they are user-runtime entries named provider:<id>.
SELECT id, name, base_url, models
FROM providers
ORDER BY created_at ASC, id ASC;

-- name: GetProvider :one
-- Row for one provider; sql.ErrNoRows = unknown id.
SELECT id, name, base_url, models
FROM providers WHERE id = ?;

-- name: CreateProvider :exec
-- id and created_at are stamped by the caller/database.
INSERT INTO providers (id, name, base_url, models)
VALUES (?, ?, ?, ?);

-- name: UpdateProvider :exec
UPDATE providers
SET name = ?, base_url = ?, models = ?
WHERE id = ?;

-- name: DeleteProvider :exec
DELETE FROM providers WHERE id = ?;
