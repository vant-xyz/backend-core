ALTER TABLE balances
    ADD COLUMN IF NOT EXISTS locked_balance_real FLOAT8 NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS locked_balance_demo FLOAT8 NOT NULL DEFAULT 0;

UPDATE balances
SET locked_balance_real = locked_balance,
    locked_balance_demo = 0
WHERE (locked_balance_real = 0 AND locked_balance_demo = 0)
  AND locked_balance <> 0;

ALTER TABLE vs_events
    ADD COLUMN IF NOT EXISTS is_demo BOOLEAN NOT NULL DEFAULT FALSE;

