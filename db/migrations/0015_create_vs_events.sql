CREATE TABLE IF NOT EXISTS vs_events (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    creator_email TEXT NOT NULL,
    mode TEXT NOT NULL,
    threshold INTEGER NOT NULL,
    stake_amount NUMERIC(18,6) NOT NULL,
    participant_target INTEGER NOT NULL,
    status TEXT NOT NULL,
    outcome TEXT NOT NULL DEFAULT '',
    outcome_description TEXT NOT NULL DEFAULT '',
    creation_tx_hash TEXT NOT NULL DEFAULT '',
    settlement_tx_hash TEXT NOT NULL DEFAULT '',
    chain_state TEXT NOT NULL DEFAULT 'PENDING_CHAIN_CREATE',
    join_deadline_utc TIMESTAMPTZ NOT NULL,
    resolve_deadline_utc TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_vs_events_creator ON vs_events (creator_email);
CREATE INDEX IF NOT EXISTS idx_vs_events_status ON vs_events (status);

CREATE TABLE IF NOT EXISTS vs_event_participants (
    id TEXT PRIMARY KEY,
    vs_event_id TEXT NOT NULL REFERENCES vs_events(id) ON DELETE CASCADE,
    user_email TEXT NOT NULL,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_amount NUMERIC(18,6) NOT NULL,
    confirmation TEXT NOT NULL DEFAULT '',
    confirmed_at TIMESTAMPTZ,
    UNIQUE(vs_event_id, user_email)
);

CREATE INDEX IF NOT EXISTS idx_vs_participants_event ON vs_event_participants (vs_event_id);
