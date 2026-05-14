INSERT INTO vantic_points_ledger (user_email, is_demo, action, points, ref_id, created_at)

SELECT user_email, is_demo, 'trade_executed', 5.0, id || '_executed', created_at
FROM positions

UNION ALL

SELECT user_email, is_demo, 'trade_won', 20.0, id || '_won', updated_at
FROM positions
WHERE status = 'SETTLED' AND realized_pnl > 0

UNION ALL

SELECT user_email, is_demo, 'trade_lost', -20.0, id || '_lost', updated_at
FROM positions
WHERE status = 'SETTLED' AND realized_pnl <= 0

UNION ALL

SELECT user_email, is_demo, 'trade_executed', 5.0, id || '_close_executed', updated_at
FROM positions
WHERE status = 'CLOSED'

UNION ALL

SELECT user_email, is_demo, 'trade_won', 20.0, id || '_close_won', updated_at
FROM positions
WHERE status = 'CLOSED' AND realized_pnl > 0

UNION ALL

SELECT user_email, is_demo, 'trade_lost', -20.0, id || '_close_lost', updated_at
FROM positions
WHERE status = 'CLOSED' AND realized_pnl < 0

UNION ALL

SELECT e.creator_email, e.is_demo, 'vs_created', 150.0, e.id || '_vs_created', e.created_at
FROM vs_events e

UNION ALL

SELECT p.user_email, e.is_demo, 'vs_joined', 125.0, e.id || '_' || p.user_email || '_vs_joined', p.joined_at
FROM vs_event_participants p
JOIN vs_events e ON e.id = p.vs_event_id
WHERE p.user_email != e.creator_email

UNION ALL

SELECT p.user_email, e.is_demo, 'vs_won', 200.0, p.id || '_vs_settle', e.resolved_at
FROM vs_event_participants p
JOIN vs_events e ON e.id = p.vs_event_id
WHERE e.outcome IS NOT NULL AND e.outcome != '' AND p.confirmation = e.outcome

UNION ALL

SELECT p.user_email, e.is_demo, 'vs_lost', 100.0, p.id || '_vs_settle', e.resolved_at
FROM vs_event_participants p
JOIN vs_events e ON e.id = p.vs_event_id
WHERE e.outcome IS NOT NULL AND e.outcome != '' AND p.confirmation != e.outcome

UNION ALL

SELECT
    user_email,
    (nature = 'demo'),
    'deposit',
    LEAST(50.0 * POWER(1.1, LEAST(amount - 1.0, 100.0)), 9000000.0),
    id,
    created_at
FROM transactions
WHERE type IN ('deposit', 'faucet') AND amount > 0

UNION ALL

SELECT
    user_email,
    (nature = 'demo'),
    'withdrawal',
    LEAST(25.0 * POWER(1.3, LEAST(amount - 1.0, 50.0)), 9000000.0),
    id,
    created_at
FROM transactions
WHERE type = 'withdrawal' AND amount > 0

UNION ALL

SELECT
    user_email,
    (nature = 'demo'),
    'asset_sale',
    LEAST(60.0 * POWER(1.7, LEAST(amount - 1.0, 30.0)), 9000000.0),
    id,
    created_at
FROM transactions
WHERE type = 'asset_withdrawal' AND amount > 0

ON CONFLICT (user_email, action, ref_id) WHERE ref_id IS NOT NULL DO NOTHING;
