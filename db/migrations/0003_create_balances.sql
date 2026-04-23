CREATE TABLE IF NOT EXISTS balances (
    id             TEXT PRIMARY KEY,
    email          TEXT NOT NULL REFERENCES users(email) ON DELETE CASCADE,
    usdc_sol       FLOAT8 NOT NULL DEFAULT 0,
    usdc_base      FLOAT8 NOT NULL DEFAULT 0,
    usdt_sol       FLOAT8 NOT NULL DEFAULT 0,
    usdg_sol       FLOAT8 NOT NULL DEFAULT 0,
    sol            FLOAT8 NOT NULL DEFAULT 0,
    eth_base       FLOAT8 NOT NULL DEFAULT 0,
    naira          FLOAT8 NOT NULL DEFAULT 0,
    demo_usdc_sol  FLOAT8 NOT NULL DEFAULT 0,
    demo_sol       FLOAT8 NOT NULL DEFAULT 0,
    demo_naira     FLOAT8 NOT NULL DEFAULT 0,
    vnaira         FLOAT8 NOT NULL DEFAULT 0,
    locked_balance FLOAT8 NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_balances_email ON balances(email);
