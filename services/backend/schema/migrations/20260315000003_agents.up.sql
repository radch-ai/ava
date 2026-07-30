-- Agents table
--
-- Design:
--   - Composite PK (user_id, name) - no UUID for agent ID
--   - user_id is Zitadel's Sonyflake ID (BIGINT)
--   - name is user-friendly string (e.g., "ava", "worker-1")
--   - Duplicate names auto-suffixed: name-2, name-3, etc.
--   - Icon stored as separate column (optional)
--   - Config stored as JSONB for flexibility
CREATE TABLE IF NOT EXISTS agents (
    user_id    BIGINT NOT NULL,
    name       TEXT NOT NULL,
    icon       TEXT,
    config     JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (user_id, name),
    FOREIGN KEY (user_id) REFERENCES users(user_id) ON DELETE CASCADE
);
