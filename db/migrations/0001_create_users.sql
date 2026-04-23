CREATE TABLE IF NOT EXISTS users (
    email             TEXT PRIMARY KEY,
    name              TEXT NOT NULL DEFAULT '',
    full_name         TEXT NOT NULL DEFAULT '',
    username          TEXT UNIQUE NOT NULL DEFAULT '',
    password          TEXT NOT NULL,
    vant_id           TEXT NOT NULL DEFAULT '',
    balance_id        TEXT NOT NULL DEFAULT '',
    socials           TEXT[] NOT NULL DEFAULT '{}',
    profile_image_url TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);
