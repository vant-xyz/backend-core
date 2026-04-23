CREATE TABLE IF NOT EXISTS markets (
    id                  TEXT PRIMARY KEY,
    market_type         TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL DEFAULT 'active',
    quote_currency      TEXT NOT NULL DEFAULT 'USD',
    title               TEXT NOT NULL DEFAULT '',
    description         TEXT NOT NULL DEFAULT '',
    data_provider       TEXT NOT NULL DEFAULT '',
    creator_address     TEXT NOT NULL DEFAULT '',
    market_pda          TEXT NOT NULL DEFAULT '',
    start_time_utc      TIMESTAMPTZ NOT NULL,
    end_time_utc        TIMESTAMPTZ NOT NULL,
    duration_seconds    BIGINT NOT NULL DEFAULT 0,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    creation_tx_hash    TEXT NOT NULL DEFAULT '',
    asset               TEXT NOT NULL DEFAULT '',
    direction           TEXT NOT NULL DEFAULT '',
    target_price        BIGINT NOT NULL DEFAULT 0,
    current_price       BIGINT NOT NULL DEFAULT 0,
    outcome             TEXT NOT NULL DEFAULT '',
    outcome_description TEXT NOT NULL DEFAULT '',
    end_price           BIGINT NOT NULL DEFAULT 0,
    settlement_tx_hash  TEXT NOT NULL DEFAULT '',
    resolved_at         TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_markets_status         ON markets(status);
CREATE INDEX IF NOT EXISTS idx_markets_type_status    ON markets(market_type, status);
CREATE INDEX IF NOT EXISTS idx_markets_asset          ON markets(asset);
CREATE INDEX IF NOT EXISTS idx_markets_end_time       ON markets(end_time_utc);
