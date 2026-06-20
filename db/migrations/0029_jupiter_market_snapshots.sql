CREATE TABLE IF NOT EXISTS jupiter_market_snapshots (
    id           BIGSERIAL    PRIMARY KEY,
    event_id     TEXT         NOT NULL,
    market_id    TEXT         NOT NULL,
    market_title TEXT         NOT NULL,
    yes_price    BIGINT       NOT NULL, -- micro-USD (1_000_000 = $1.00 = 100¢)
    no_price     BIGINT       NOT NULL,
    recorded_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_jms_event_recorded ON jupiter_market_snapshots (event_id, recorded_at DESC);
CREATE INDEX IF NOT EXISTS idx_jms_market_recorded ON jupiter_market_snapshots (market_id, recorded_at DESC);
