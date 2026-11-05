-- A connection a model call resolves to. ids are server-minted
-- uuids; api_key '' = not configured yet.
CREATE TABLE IF NOT EXISTS providers (
    id         TEXT NOT NULL PRIMARY KEY,
    name       TEXT NOT NULL,
    base_url   TEXT NOT NULL,
    api_key    TEXT NOT NULL DEFAULT '',
    created_at BIGINT NOT NULL
        DEFAULT (CAST((julianday('now') - 2440587.5) * 86400000000 AS INTEGER))
);
