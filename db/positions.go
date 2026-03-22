package db

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5"
	"github.com/vant-xyz/backend-code/models"
)

const positionColumns = `
	id, user_email, market_id, side, shares, avg_entry_price,
	realized_pnl, payout_amount, status, quote_currency,
	created_at, updated_at, settled_at
`

func SavePosition(ctx context.Context, p *models.Position) error {
	_, err := Pool.Exec(ctx, `
		INSERT INTO positions (
			id, user_email, market_id, side, shares, avg_entry_price,
			realized_pnl, payout_amount, status, quote_currency,
			created_at, updated_at, settled_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
	`,
		p.ID, p.UserEmail, p.MarketID, string(p.Side), p.Shares,
		p.AvgEntryPrice, p.RealizedPnL, p.PayoutAmount, string(p.Status),
		p.QuoteCurrency, p.CreatedAt, p.UpdatedAt, p.SettledAt,
	)
	if err != nil {
		return fmt.Errorf("failed to save position %s: %w", p.ID, err)
	}
	return nil
}

func GetPositionByID(ctx context.Context, positionID string) (*models.Position, error) {
	row := Pool.QueryRow(ctx,
		`SELECT `+positionColumns+` FROM positions WHERE id = $1`, positionID,
	)
	p, err := scanPosition(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("position not found: %s", positionID)
		}
		return nil, fmt.Errorf("failed to get position %s: %w", positionID, err)
	}
	return p, nil
}

func GetUserPositionForMarketSide(ctx context.Context, userEmail, marketID string, side models.OrderSide) (*models.Position, error) {
	row := Pool.QueryRow(ctx, `
		SELECT `+positionColumns+`
		FROM positions
		WHERE user_email = $1 AND market_id = $2 AND side = $3 AND status = 'ACTIVE'
		LIMIT 1
	`, userEmail, marketID, string(side))

	p, err := scanPosition(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get position: %w", err)
	}
	return p, nil
}

func UpdatePosition(ctx context.Context, positionID string, shares, avgEntryPrice float64) error {
	_, err := Pool.Exec(ctx, `
		UPDATE positions SET shares = $1, avg_entry_price = $2, updated_at = NOW()
		WHERE id = $3
	`, shares, avgEntryPrice, positionID)
	return err
}

func SettlePositionRecord(ctx context.Context, positionID string, payout, realizedPnL float64) error {
	_, err := Pool.Exec(ctx, `
		UPDATE positions
		SET status = 'SETTLED', payout_amount = $1, realized_pnl = $2,
		    settled_at = NOW(), updated_at = NOW()
		WHERE id = $3
	`, payout, realizedPnL, positionID)
	return err
}

func GetUserPositions(ctx context.Context, userEmail, marketID string) ([]models.Position, error) {
	if marketID != "" {
		rows, err := Pool.Query(ctx,
			`SELECT `+positionColumns+` FROM positions WHERE user_email = $1 AND market_id = $2 ORDER BY created_at DESC`,
			userEmail, marketID,
		)
		if err != nil {
			return nil, err
		}
		return scanPositions(rows)
	}
	rows, err := Pool.Query(ctx,
		`SELECT `+positionColumns+` FROM positions WHERE user_email = $1 ORDER BY created_at DESC`,
		userEmail,
	)
	if err != nil {
		return nil, err
	}
	return scanPositions(rows)
}

func GetMarketPositions(ctx context.Context, marketID string, status models.PositionStatus) ([]models.Position, error) {
	rows, err := Pool.Query(ctx,
		`SELECT `+positionColumns+` FROM positions WHERE market_id = $1 AND status = $2`,
		marketID, string(status),
	)
	if err != nil {
		return nil, err
	}
	return scanPositions(rows)
}

type positionScanner interface {
	Scan(dest ...any) error
}

func scanPosition(row positionScanner) (*models.Position, error) {
	var p models.Position
	var side, status string

	err := row.Scan(
		&p.ID, &p.UserEmail, &p.MarketID, &side, &p.Shares,
		&p.AvgEntryPrice, &p.RealizedPnL, &p.PayoutAmount, &status,
		&p.QuoteCurrency, &p.CreatedAt, &p.UpdatedAt, &p.SettledAt,
	)
	if err != nil {
		return nil, err
	}

	p.Side = models.OrderSide(side)
	p.Status = models.PositionStatus(status)

	return &p, nil
}

func scanPositions(rows pgx.Rows) ([]models.Position, error) {
	defer rows.Close()
	var positions []models.Position
	for rows.Next() {
		p, err := scanPosition(rows)
		if err != nil {
			log.Printf("[DB] Failed to scan position row: %v", err)
			continue
		}
		positions = append(positions, *p)
	}
	return positions, rows.Err()
}