-- The model catalog. PK (provider_id, model_id); UNIQUE (model_id)
-- keeps an id single-provider. The heartbeat/cron FKs target that
-- UNIQUE.
CREATE TABLE IF NOT EXISTS models (
    provider_id TEXT NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
    model_id    TEXT NOT NULL,
    PRIMARY KEY (provider_id, model_id),
    UNIQUE (model_id)
);
