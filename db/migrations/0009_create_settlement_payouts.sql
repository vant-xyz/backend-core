CREATE TABLE IF NOT EXISTS settlement_payouts (
    id             TEXT PRIMARY KEY,
    market_id      TEXT NOT NULL REFERENCES markets(id),
    position_id    TEXT NOT NULL REFERENCES positions(id),
    user_email     TEXT NOT NULL,
    shares         FLOAT8 NOT NULL DEFAULT 0,
    payout_amount  FLOAT8 NOT NULL DEFAULT 0,
    quote_currency TEXT NOT NULL DEFAULT 'USD',
    processed      BOOLEAN NOT NULL DEFAULT FALSE,
    processed_at   TIMESTAMPTZ,
    UNIQUE(market_id, position_id)
);

CREATE INDEX IF NOT EXISTS idx_settlement_payouts_market    ON settlement_payouts(market_id);
CREATE INDEX IF NOT EXISTS idx_settlement_payouts_user      ON settlement_payouts(user_email);
CREATE INDEX IF NOT EXISTS idx_settlement_payouts_processed ON settlement_payouts(processed);
