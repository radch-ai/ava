-- A connection a model call resolves to. ids are server-minted
-- uuids (system providers attach with their slug id); keys live in the
-- vault as runtime entries, never in this table. models is the
-- provider's custom model ids as a JSON array — system models are
-- fetched, never stored.
CREATE TABLE IF NOT EXISTS providers (
    id         TEXT NOT NULL PRIMARY KEY,
    name       TEXT NOT NULL,
    base_url   TEXT NOT NULL,
    models     TEXT NOT NULL DEFAULT '[]',
    created_at BIGINT NOT NULL
        DEFAULT (CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER))
);
