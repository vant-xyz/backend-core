ALTER TABLE markets
    ADD COLUMN IF NOT EXISTS asset_image          TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS market_image_small   TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS market_image_banner  TEXT NOT NULL DEFAULT '';
