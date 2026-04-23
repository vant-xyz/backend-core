CREATE TABLE IF NOT EXISTS waitlist (
    email          TEXT PRIMARY KEY,
    referral_code  TEXT UNIQUE NOT NULL DEFAULT '',
    referred_by    TEXT NOT NULL DEFAULT '',
    referral_count INT NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_waitlist_referral_code ON waitlist(referral_code);
