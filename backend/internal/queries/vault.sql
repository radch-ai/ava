-- name: GetVaultKeyset :one
-- The single wrapped keyset row; sql.ErrNoRows = first boot.
SELECT wrapped_keyset FROM vault_keyset WHERE id = 1;

-- name: InsertVaultKeyset :exec
-- First boot only; id is pinned to 1 by the table CHECK.
INSERT INTO vault_keyset (id, wrapped_keyset) VALUES (1, ?);

-- name: UpdateVaultKeyset :exec
-- Keyset rotation rewrites the single wrapped blob in place.
UPDATE vault_keyset SET wrapped_keyset = ? WHERE id = 1;

-- name: PutVaultEntry :exec
-- Upsert at the (vault, name) address.
INSERT INTO vault_entries (vault, name, kind, envelope, meta, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (vault, name) DO UPDATE SET
    kind = excluded.kind,
    envelope = excluded.envelope,
    meta = excluded.meta,
    updated_at = excluded.updated_at;

-- name: GetVaultEntry :one
-- Envelope + metadata only; sql.ErrNoRows = unknown address.
SELECT envelope, meta FROM vault_entries
WHERE vault = ? AND name = ?;

-- name: ReplaceVaultEntry :execrows
-- Update-only; 0 rows = entry does not exist.
UPDATE vault_entries
SET envelope = ?, meta = ?, updated_at = ?
WHERE vault = ? AND name = ?;

-- name: DeleteVaultEntry :execrows
-- 0 rows = entry does not exist.
DELETE FROM vault_entries WHERE vault = ? AND name = ?;

-- name: ListVaultEntries :many
-- Metadata for vault, ordered by name; never the envelope.
SELECT name, kind, meta, updated_at FROM vault_entries
WHERE vault = ?
ORDER BY name;
