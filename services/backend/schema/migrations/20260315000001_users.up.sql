-- Users table
-- Stores user identity and payment verification keys.
-- user_id is Zitadel's Sonyflake ID (BIGINT stored as 64-bit integer).
CREATE TABLE IF NOT EXISTS users (
    user_id         BIGINT PRIMARY KEY,
    subscription_vk TEXT,
    topup_vk        TEXT
);
