package db

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5"
	"github.com/vant-xyz/backend-code/models"
)

const orderColumns = `
	id, user_email, market_id, side, type, price, quantity,
	filled_qty, remaining_qty, status, quote_currency,
	created_at, updated_at, expires_at
`

func SaveOrder(ctx context.Context, o *models.Order) error {
	_, err := Pool.Exec(ctx, `
		INSERT INTO orders (
			id, user_email, market_id, side, type, price, quantity,
			filled_qty, remaining_qty, status, quote_currency,
			created_at, updated_at, expires_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
	`,
		o.ID, o.UserEmail, o.MarketID, string(o.Side), string(o.Type),
		o.Price, o.Quantity, o.FilledQty, o.RemainingQty, string(o.Status),
		o.QuoteCurrency, o.CreatedAt, o.UpdatedAt, o.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("failed to save order %s: %w", o.ID, err)
	}
	return nil
}

func GetOrderByID(ctx context.Context, orderID string) (*models.Order, error) {
	row := Pool.QueryRow(ctx,
		`SELECT `+orderColumns+` FROM orders WHERE id = $1`, orderID,
	)
	o, err := scanOrder(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("order not found: %s", orderID)
		}
		return nil, fmt.Errorf("failed to get order %s: %w", orderID, err)
	}
	return o, nil
}

func UpdateOrderFill(ctx context.Context, orderID string, filledQty, remainingQty float64, status models.OrderStatus) error {
	_, err := Pool.Exec(ctx, `
		UPDATE orders
		SET filled_qty = $1, remaining_qty = $2, status = $3, updated_at = NOW()
		WHERE id = $4 AND status NOT IN ('FILLED', 'CANCELLED')
	`, filledQty, remainingQty, string(status), orderID)
	return err
}

func UpdateOrderStatus(ctx context.Context, orderID string, status models.OrderStatus) error {
	_, err := Pool.Exec(ctx, `
		UPDATE orders SET status = $1, updated_at = NOW() WHERE id = $2
	`, string(status), orderID)
	return err
}

func GetUserOrders(ctx context.Context, userEmail, marketID string) ([]models.Order, error) {
	if marketID != "" {
		rows, err := Pool.Query(ctx,
			`SELECT `+orderColumns+` FROM orders WHERE user_email = $1 AND market_id = $2 ORDER BY created_at DESC`,
			userEmail, marketID,
		)
		if err != nil {
			return nil, err
		}
		return scanOrders(rows)
	}
	rows, err := Pool.Query(ctx,
		`SELECT `+orderColumns+` FROM orders WHERE user_email = $1 ORDER BY created_at DESC`,
		userEmail,
	)
	if err != nil {
		return nil, err
	}
	return scanOrders(rows)
}

func GetMarketOrders(ctx context.Context, marketID string, status models.OrderStatus) ([]models.Order, error) {
	rows, err := Pool.Query(ctx,
		`SELECT `+orderColumns+` FROM orders WHERE market_id = $1 AND status = $2 ORDER BY created_at ASC`,
		marketID, string(status),
	)
	if err != nil {
		return nil, err
	}
	return scanOrders(rows)
}

func GetOpenOrdersForMarket(ctx context.Context, marketID string) ([]models.Order, error) {
	rows, err := Pool.Query(ctx,
		`SELECT `+orderColumns+` FROM orders WHERE market_id = $1 AND status IN ('OPEN', 'PARTIALLY_FILLED') ORDER BY created_at ASC`,
		marketID,
	)
	if err != nil {
		return nil, err
	}
	return scanOrders(rows)
}

func GetMarketFilledOrders(ctx context.Context, marketID string) ([]models.Order, error) {
	rows, err := Pool.Query(ctx,
		`SELECT `+orderColumns+` FROM orders WHERE market_id = $1 AND status IN ('FILLED', 'PARTIALLY_FILLED') ORDER BY updated_at ASC`,
		marketID,
	)
	if err != nil {
		return nil, err
	}
	return scanOrders(rows)
}

func GetMarketTrades(ctx context.Context, marketID string, limit int) ([]models.Order, error) {
	rows, err := Pool.Query(ctx,
		`SELECT `+orderColumns+` FROM orders WHERE market_id = $1 AND status = 'FILLED' ORDER BY updated_at DESC LIMIT $2`,
		marketID, limit,
	)
	if err != nil {
		return nil, err
	}
	return scanOrders(rows)
}

type orderScanner interface {
	Scan(dest ...any) error
}

func scanOrder(row orderScanner) (*models.Order, error) {
	var o models.Order
	var side, orderType, status string

	err := row.Scan(
		&o.ID, &o.UserEmail, &o.MarketID, &side, &orderType,
		&o.Price, &o.Quantity, &o.FilledQty, &o.RemainingQty, &status,
		&o.QuoteCurrency, &o.CreatedAt, &o.UpdatedAt, &o.ExpiresAt,
	)
	if err != nil {
		return nil, err
	}

	o.Side = models.OrderSide(side)
	o.Type = models.OrderType(orderType)
	o.Status = models.OrderStatus(status)

	return &o, nil
}

func scanOrders(rows pgx.Rows) ([]models.Order, error) {
	defer rows.Close()
	var orders []models.Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			log.Printf("[DB] Failed to scan order row: %v", err)
			continue
		}
		orders = append(orders, *o)
	}
	return orders, rows.Err()
}