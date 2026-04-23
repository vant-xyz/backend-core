CREATE TABLE IF NOT EXISTS orders (
    id             TEXT PRIMARY KEY,
    user_email     TEXT NOT NULL,
    market_id      TEXT NOT NULL REFERENCES markets(id),
    side           TEXT NOT NULL,
    type           TEXT NOT NULL,
    price          FLOAT8 NOT NULL DEFAULT 0,
    quantity       FLOAT8 NOT NULL DEFAULT 0,
    filled_qty     FLOAT8 NOT NULL DEFAULT 0,
    remaining_qty  FLOAT8 NOT NULL DEFAULT 0,
    status         TEXT NOT NULL DEFAULT 'OPEN',
    quote_currency TEXT NOT NULL DEFAULT 'USD',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_orders_user_email   ON orders(user_email);
CREATE INDEX IF NOT EXISTS idx_orders_market_id    ON orders(market_id);
CREATE INDEX IF NOT EXISTS idx_orders_market_status ON orders(market_id, status);
CREATE INDEX IF NOT EXISTS idx_orders_user_market  ON orders(user_email, market_id);
