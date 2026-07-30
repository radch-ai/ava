CREATE TABLE IF NOT EXISTS lyla_thread (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id BIGINT NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (user_id) REFERENCES users(user_id) ON DELETE CASCADE,
    CHECK (role IN ('user', 'assistant'))
);

CREATE INDEX IF NOT EXISTS idx_lyla_thread_user_created
    ON lyla_thread(user_id, created_at ASC, id ASC);
