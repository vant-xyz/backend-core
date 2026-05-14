package db

import (
	"context"
	"fmt"
	"time"
)

type VPAction string

const (
	VPTradeExecuted VPAction = "trade_executed"
	VPTradeWon      VPAction = "trade_won"
	VPTradeLost     VPAction = "trade_lost"
	VPDeposit       VPAction = "deposit"
	VPWithdrawal    VPAction = "withdrawal"
	VPAssetSale     VPAction = "asset_sale"
	VPVSCreated     VPAction = "vs_created"
	VPVSJoined      VPAction = "vs_joined"
	VPVSWon         VPAction = "vs_won"
	VPVSLost        VPAction = "vs_lost"
)

func AwardVanticPoints(ctx context.Context, userEmail string, isDemo bool, action VPAction, points float64, refID string) error {
	if Pool == nil {
		return nil
	}
	var refPtr *string
	if refID != "" {
		refPtr = &refID
	}
	_, err := Pool.Exec(ctx,
		`INSERT INTO vantic_points_ledger (user_email, is_demo, action, points, ref_id, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (user_email, action, ref_id) WHERE ref_id IS NOT NULL DO NOTHING`,
		userEmail, isDemo, string(action), points, refPtr, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("award vantic points: %w", err)
	}
	return nil
}

func GetUserVanticPoints(ctx context.Context, userEmail string, isDemo bool) (float64, error) {
	if Pool == nil {
		return 0, nil
	}
	var total float64
	err := Pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(points), 0) FROM vantic_points_ledger WHERE user_email = $1 AND is_demo = $2`,
		userEmail, isDemo,
	).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("get vantic points: %w", err)
	}
	return total, nil
}
