CREATE TABLE IF NOT EXISTS positions (
    id              TEXT PRIMARY KEY,
    user_email      TEXT NOT NULL,
    market_id       TEXT NOT NULL REFERENCES markets(id),
    side            TEXT NOT NULL,
    shares          FLOAT8 NOT NULL DEFAULT 0,
    avg_entry_price FLOAT8 NOT NULL DEFAULT 0,
    realized_pnl    FLOAT8 NOT NULL DEFAULT 0,
    payout_amount   FLOAT8 NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'ACTIVE',
    quote_currency  TEXT NOT NULL DEFAULT 'USD',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    settled_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_positions_user_email    ON positions(user_email);
CREATE INDEX IF NOT EXISTS idx_positions_market_id     ON positions(market_id);
CREATE INDEX IF NOT EXISTS idx_positions_market_status ON positions(market_id, status);

-- Prevents duplicate active positions for the same user/market/side
CREATE UNIQUE INDEX IF NOT EXISTS idx_positions_user_market_side_active
    ON positions(user_email, market_id, side)
    WHERE status = 'ACTIVE';
