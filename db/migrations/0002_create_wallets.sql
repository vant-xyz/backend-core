CREATE TABLE IF NOT EXISTS wallets (
    account_id           TEXT PRIMARY KEY,
    email                TEXT NOT NULL REFERENCES users(email) ON DELETE CASCADE,
    sol_public_key       TEXT NOT NULL DEFAULT '',
    sol_private_key      TEXT NOT NULL DEFAULT '',
    base_public_key      TEXT NOT NULL DEFAULT '',
    base_private_key     TEXT NOT NULL DEFAULT '',
    naira_account_number TEXT NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_wallets_email ON wallets(email);
CREATE INDEX IF NOT EXISTS idx_wallets_sol_public_key ON wallets(sol_public_key);
