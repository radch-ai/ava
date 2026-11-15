-- The global default model. One row: id is pinned to 1, so the
-- primary key is also the singleton rule. The pair follows the
-- heartbeat convention (model id + owning provider row) but is
-- NOT NULL — a default is always complete. Empty table = no
-- default: nil selections fail closed. Deleting the provider
-- cascades the row away, cleanly unsetting the default.

CREATE TABLE IF NOT EXISTS default_model (
    id          INTEGER PRIMARY KEY CHECK (id = 1),
    model       TEXT NOT NULL,
    provider_id TEXT NOT NULL REFERENCES providers(id) ON DELETE CASCADE
);
