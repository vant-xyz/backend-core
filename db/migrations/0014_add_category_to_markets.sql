ALTER TABLE markets ADD COLUMN IF NOT EXISTS category TEXT NOT NULL DEFAULT 'general';

UPDATE markets SET category = 'crypto' WHERE market_type = 'CAPPM';
