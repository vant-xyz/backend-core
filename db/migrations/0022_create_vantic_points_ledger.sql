CREATE TABLE IF NOT EXISTS vantic_points_ledger (
    id          BIGSERIAL     PRIMARY KEY,
    user_email  TEXT          NOT NULL,
    is_demo     BOOLEAN       NOT NULL DEFAULT FALSE,
    action      TEXT          NOT NULL,
    points      NUMERIC(14,4) NOT NULL,
    ref_id      TEXT,
    created_at  TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_vpl_user ON vantic_points_ledger (user_email, is_demo);
CREATE INDEX IF NOT EXISTS idx_vpl_action ON vantic_points_ledger (action);
CREATE UNIQUE INDEX IF NOT EXISTS idx_vpl_ref ON vantic_points_ledger (user_email, action, ref_id) WHERE ref_id IS NOT NULL;
