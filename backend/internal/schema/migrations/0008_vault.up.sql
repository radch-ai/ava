-- Encrypted secret storage (backend/internal/vault). vault_keyset is the single
-- wrapped Tink keyset; the master key that wraps it never lives in the
-- DB. vault_entries: secrets sealed to their (vault, name) address —
-- vault is "runtime" (app-managed keys, e.g. provider api keys) or
-- "secrets" (user-stored, for agents to read later), enforced by CHECK.
CREATE TABLE IF NOT EXISTS vault_keyset (
    id             INTEGER NOT NULL PRIMARY KEY CHECK (id = 1),
    wrapped_keyset TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS vault_entries (
    vault      TEXT   NOT NULL CHECK (vault IN ('runtime', 'secrets')),
    name       TEXT   NOT NULL,
    kind       TEXT   NOT NULL,
    envelope   TEXT   NOT NULL,
    meta       TEXT   NOT NULL DEFAULT '',
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (vault, name)
);
