-- name: ListProviders :many
-- Providers in creation order. Provider keys never leave the vault:
-- they are user-runtime entries named provider:<id>.
SELECT id, name, base_url, models, discovered_context_windows
FROM providers
ORDER BY created_at ASC, id ASC;

-- name: GetProvider :one
-- Row for one provider; sql.ErrNoRows = unknown id.
SELECT id, name, base_url, models, discovered_context_windows
FROM providers WHERE id = ?;

-- name: CreateProvider :exec
-- id and created_at are stamped by the caller/database.
INSERT INTO providers (id, name, base_url, models, discovered_context_windows)
VALUES (?, ?, ?, ?, ?);

-- name: UpdateProvider :exec
-- discovery owns discovered_context_windows; a save never writes it.
UPDATE providers
SET name = ?, base_url = ?, models = ?
WHERE id = ?;

-- name: SetDiscoveredContextWindows :exec
-- a wholesale clear of one provider's learned windows: the repoint
-- path only: limits taught by the departed endpoint must not survive.
UPDATE providers SET discovered_context_windows = ?
WHERE id = ?;

-- name: SetDiscoveredContextWindow :exec
-- one reactively-learned window merged in place, so concurrent discovery
-- writers cannot drop each other's entries.
UPDATE providers
SET discovered_context_windows = json_set(discovered_context_windows, sqlc.arg(path), sqlc.arg(window))
WHERE id = sqlc.arg(id);

-- name: DeleteProvider :exec
DELETE FROM providers WHERE id = ?;
