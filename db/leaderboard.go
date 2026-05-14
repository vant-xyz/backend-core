package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type LeaderboardEntry struct {
	Rank            int     `json:"rank"`
	Email           string  `json:"email"`
	Username        string  `json:"username"`
	ProfileImageURL string  `json:"profile_image_url"`
	PnL             float64 `json:"pnl"`
	Trades          int64   `json:"trades"`
	Deposits        float64 `json:"deposits"`
	Withdrawals     float64 `json:"withdrawals"`
	VanticPoints    float64 `json:"vantic_points"`
	ActivityScore   float64 `json:"activity_score"`
}

func GetLeaderboard(ctx context.Context, isDemo bool, limit int) ([]LeaderboardEntry, error) {
	cacheKey := fmt.Sprintf("leaderboard:demo=%v:limit=%d", isDemo, limit)

	if RDB != nil {
		if cached, err := RDB.Get(ctx, cacheKey).Bytes(); err == nil {
			var entries []LeaderboardEntry
			if json.Unmarshal(cached, &entries) == nil {
				return entries, nil
			}
		}
	}

	rows, err := Pool.Query(ctx, `
		WITH
		vp_data AS (
			SELECT user_email, COALESCE(SUM(points), 0) AS vantic_points
			FROM vantic_points_ledger
			WHERE is_demo = $1
			GROUP BY user_email
		),
		pnl_data AS (
			SELECT user_email, COALESCE(SUM(realized_pnl), 0) AS total_pnl
			FROM positions
			WHERE is_demo = $1 AND status = 'SETTLED'
			GROUP BY user_email
		),
		trade_data AS (
			SELECT user_email, COUNT(*) AS trade_count
			FROM positions
			WHERE is_demo = $1
			GROUP BY user_email
		),
		deposit_data AS (
			SELECT user_email, COALESCE(SUM(amount), 0) AS total_deposits
			FROM transactions
			WHERE type IN ('deposit', 'faucet')
			GROUP BY user_email
		),
		withdrawal_data AS (
			SELECT user_email, COALESCE(SUM(amount), 0) AS total_withdrawals
			FROM transactions
			WHERE type IN ('withdrawal', 'asset_withdrawal')
			GROUP BY user_email
		),
		raw AS (
			SELECT
				u.email,
				u.username,
				COALESCE(u.profile_image_url, '')  AS profile_image_url,
				COALESCE(p.total_pnl, 0)           AS pnl,
				COALESCE(t.trade_count, 0)          AS trades,
				COALESCE(d.total_deposits, 0)       AS deposits,
				COALESCE(w.total_withdrawals, 0)    AS withdrawals,
				COALESCE(vp.vantic_points, 0)       AS vantic_points
			FROM users u
			LEFT JOIN pnl_data p        ON p.user_email = u.email
			LEFT JOIN trade_data t      ON t.user_email = u.email
			LEFT JOIN deposit_data d    ON d.user_email = u.email
			LEFT JOIN withdrawal_data w ON w.user_email = u.email
			LEFT JOIN vp_data vp        ON vp.user_email = u.email
			WHERE u.username != ''
		),
		ranked AS (
			SELECT *,
				PERCENT_RANK() OVER (ORDER BY pnl)         AS pnl_rank,
				PERCENT_RANK() OVER (ORDER BY trades)      AS trades_rank,
				PERCENT_RANK() OVER (ORDER BY deposits)    AS deposits_rank,
				PERCENT_RANK() OVER (ORDER BY withdrawals) AS withdrawals_rank
			FROM raw
		)
		SELECT
			email,
			username,
			profile_image_url,
			pnl,
			trades,
			deposits,
			withdrawals,
			vantic_points,
			ROUND((100.0 * (0.25 * pnl_rank + 0.25 * trades_rank + 0.25 * deposits_rank + 0.25 * withdrawals_rank))::numeric, 2) AS activity_score
		FROM ranked
		ORDER BY (vantic_points + ROUND((100.0 * (0.25 * pnl_rank + 0.25 * trades_rank + 0.25 * deposits_rank + 0.25 * withdrawals_rank))::numeric, 2)) DESC
		LIMIT $2
	`, isDemo, limit)
	if err != nil {
		return nil, fmt.Errorf("leaderboard query: %w", err)
	}
	defer rows.Close()

	var entries []LeaderboardEntry
	rank := 1
	for rows.Next() {
		var e LeaderboardEntry
		if err := rows.Scan(
			&e.Email, &e.Username, &e.ProfileImageURL,
			&e.PnL, &e.Trades, &e.Deposits, &e.Withdrawals,
			&e.VanticPoints, &e.ActivityScore,
		); err != nil {
			return nil, fmt.Errorf("leaderboard scan: %w", err)
		}
		e.Rank = rank
		rank++
		entries = append(entries, e)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("leaderboard rows: %w", rows.Err())
	}

	if RDB != nil && len(entries) > 0 {
		if b, err := json.Marshal(entries); err == nil {
			RDB.Set(ctx, cacheKey, b, 60*time.Second)
		}
	}

	return entries, nil
}

func GetLeaderboardEntry(ctx context.Context, email string, isDemo bool) (*LeaderboardEntry, error) {
	var e LeaderboardEntry
	err := Pool.QueryRow(ctx, `
		WITH
		vp_data AS (
			SELECT COALESCE(SUM(points), 0) AS vantic_points
			FROM vantic_points_ledger WHERE user_email = $1 AND is_demo = $2
		),
		pnl_data AS (
			SELECT COALESCE(SUM(realized_pnl), 0) AS total_pnl
			FROM positions WHERE user_email = $1 AND is_demo = $2 AND status = 'SETTLED'
		),
		trade_data AS (
			SELECT COUNT(*) AS trade_count
			FROM positions WHERE user_email = $1 AND is_demo = $2
		),
		deposit_data AS (
			SELECT COALESCE(SUM(amount), 0) AS total_deposits
			FROM transactions WHERE user_email = $1 AND type IN ('deposit', 'faucet')
		),
		withdrawal_data AS (
			SELECT COALESCE(SUM(amount), 0) AS total_withdrawals
			FROM transactions WHERE user_email = $1 AND type IN ('withdrawal', 'asset_withdrawal')
		)
		SELECT
			u.email, u.username, COALESCE(u.profile_image_url, ''),
			p.total_pnl, t.trade_count, d.total_deposits, w.total_withdrawals,
			vp.vantic_points
		FROM users u, vp_data vp, pnl_data p, trade_data t, deposit_data d, withdrawal_data w
		WHERE u.email = $1
	`, email, isDemo).Scan(
		&e.Email, &e.Username, &e.ProfileImageURL,
		&e.PnL, &e.Trades, &e.Deposits, &e.Withdrawals,
		&e.VanticPoints,
	)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func InvalidateLeaderboardCache(ctx context.Context) {
	if RDB == nil {
		return
	}
	keys, err := RDB.Keys(ctx, "leaderboard:*").Result()
	if err != nil || len(keys) == 0 {
		return
	}
	RDB.Del(ctx, keys...)
}
