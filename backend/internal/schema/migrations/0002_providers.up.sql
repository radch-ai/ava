-- A connection a model call resolves to. ids are server-minted
-- uuids. Keys live in the vault as runtime entries, never in this
-- table. models is the provider's custom model entries as a JSON
-- array — {id, contextWindow?}; system models are fetched, never
-- stored. discovered_context_windows maps model id to the context
-- window its endpoint reported (JSON object), refreshed by discovery;
-- declared windows live on the model entries.
CREATE TABLE IF NOT EXISTS providers (
    id                          TEXT NOT NULL PRIMARY KEY,
    name                        TEXT NOT NULL,
    base_url                    TEXT NOT NULL,
    models                      TEXT NOT NULL DEFAULT '[]',
    discovered_context_windows  TEXT NOT NULL DEFAULT '{}',
    created_at                  BIGINT NOT NULL
        DEFAULT (CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER))
);
