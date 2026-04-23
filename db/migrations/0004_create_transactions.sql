CREATE TABLE IF NOT EXISTS transactions (
    id         TEXT PRIMARY KEY,
    user_email TEXT NOT NULL,
    amount     FLOAT8 NOT NULL DEFAULT 0,
    currency   TEXT NOT NULL DEFAULT '',
    nature     TEXT NOT NULL DEFAULT '',
    type       TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT '',
    tx_hash    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_transactions_user_email ON transactions(user_email);
CREATE INDEX IF NOT EXISTS idx_transactions_created_at ON transactions(created_at DESC);
