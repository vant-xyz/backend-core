package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type PayoutRecord struct {
	ID            string
	MarketID      string
	PositionID    string
	UserEmail     string
	Shares        float64
	PayoutAmount  float64
	QuoteCurrency string
	Processed     bool
}

func SavePayoutRecord(ctx context.Context, p PayoutRecord) error {
	_, err := Pool.Exec(ctx, `
		INSERT INTO settlement_payouts (id, market_id, position_id, user_email, shares, payout_amount, quote_currency, processed)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (market_id, position_id) DO NOTHING
	`, p.ID, p.MarketID, p.PositionID, p.UserEmail, p.Shares, p.PayoutAmount, p.QuoteCurrency, p.Processed)
	return err
}

func SavePayoutRecordsBatch(ctx context.Context, payouts []PayoutRecord) error {
	if len(payouts) == 0 {
		return nil
	}

	tx, err := Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin payout batch transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, p := range payouts {
		_, err := tx.Exec(ctx, `
			INSERT INTO settlement_payouts (id, market_id, position_id, user_email, shares, payout_amount, quote_currency, processed)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (market_id, position_id) DO NOTHING
		`, p.ID, p.MarketID, p.PositionID, p.UserEmail, p.Shares, p.PayoutAmount, p.QuoteCurrency, p.Processed)
		if err != nil {
			return fmt.Errorf("failed to insert payout record %s: %w", p.ID, err)
		}
	}

	return tx.Commit(ctx)
}

func GetExistingPayout(ctx context.Context, marketID, positionID string) (*PayoutRecord, error) {
	row := Pool.QueryRow(ctx, `
		SELECT id, market_id, position_id, user_email, shares, payout_amount, quote_currency, processed
		FROM settlement_payouts
		WHERE market_id = $1 AND position_id = $2
	`, marketID, positionID)

	var p PayoutRecord
	err := row.Scan(&p.ID, &p.MarketID, &p.PositionID, &p.UserEmail,
		&p.Shares, &p.PayoutAmount, &p.QuoteCurrency, &p.Processed)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get payout record: %w", err)
	}
	return &p, nil
}

func MarkPayoutProcessed(ctx context.Context, payoutID string) error {
	_, err := Pool.Exec(ctx, `
		UPDATE settlement_payouts SET processed = TRUE, processed_at = NOW() WHERE id = $1
	`, payoutID)
	return err
}