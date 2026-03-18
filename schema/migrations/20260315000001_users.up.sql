-- Users table
-- Stores user identity and payment verification keys.
CREATE TABLE IF NOT EXISTS users (
    user_id         UUID PRIMARY KEY,
    subscription_vk TEXT,
    topup_vk        TEXT
);
