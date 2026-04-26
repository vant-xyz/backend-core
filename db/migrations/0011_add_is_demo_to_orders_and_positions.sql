ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS is_demo BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE positions
    ADD COLUMN IF NOT EXISTS is_demo BOOLEAN NOT NULL DEFAULT FALSE;

DROP INDEX IF EXISTS idx_positions_user_market_side_active;

CREATE UNIQUE INDEX IF NOT EXISTS idx_positions_user_market_side_active
    ON positions(user_email, market_id, side, is_demo)
    WHERE status = 'ACTIVE';
