package db

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/vant-xyz/backend-code/models"
)

const marketColumns = `
	id, market_type, status, quote_currency, title, description,
	data_provider, creator_address, market_pda, start_time_utc, end_time_utc,
	duration_seconds, created_at, creation_tx_hash, asset, direction,
	target_price, current_price, outcome, outcome_description,
	end_price, settlement_tx_hash, resolved_at
`

func SaveMarket(ctx context.Context, m *models.Market) error {
	_, err := Pool.Exec(ctx, `
		INSERT INTO markets (
			id, market_type, status, quote_currency, title, description,
			data_provider, creator_address, market_pda, start_time_utc, end_time_utc,
			duration_seconds, created_at, creation_tx_hash, asset, direction,
			target_price, current_price, outcome, outcome_description,
			end_price, settlement_tx_hash, resolved_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,
			$17,$18,$19,$20,$21,$22,$23
		)
	`,
		m.ID, string(m.MarketType), string(m.Status), m.QuoteCurrency, m.Title,
		m.Description, m.DataProvider, m.CreatorAddress, m.MarketPDA,
		m.StartTimeUTC, m.EndTimeUTC, m.DurationSeconds, m.CreatedAt,
		m.CreationTxHash, m.Asset, string(m.Direction), m.TargetPrice,
		m.CurrentPrice, string(m.Outcome), m.OutcomeDescription,
		m.EndPrice, m.SettlementTxHash, m.ResolvedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to save market %s: %w", m.ID, err)
	}
	return nil
}

func GetMarketByID(ctx context.Context, marketID string) (*models.Market, error) {
	row := Pool.QueryRow(ctx,
		`SELECT `+marketColumns+` FROM markets WHERE id = $1`, marketID,
	)
	m, err := scanMarket(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("market not found: %s", marketID)
		}
		return nil, fmt.Errorf("failed to get market %s: %w", marketID, err)
	}
	return m, nil
}

func UpdateMarketFields(ctx context.Context, marketID string, fields map[string]interface{}) error {
	colMap := map[string]string{
		"status":              "status",
		"outcome":             "outcome",
		"outcome_description": "outcome_description",
		"end_price":           "end_price",
		"settlement_tx_hash":  "settlement_tx_hash",
		"resolved_at":         "resolved_at",
		"current_price":       "current_price",
	}

	i := 1
	setClauses := ""
	args := []interface{}{}

	for field, val := range fields {
		col, ok := colMap[field]
		if !ok {
			continue
		}
		if i > 1 {
			setClauses += ", "
		}
		setClauses += fmt.Sprintf("%s = $%d", col, i)
		args = append(args, val)
		i++
	}

	if setClauses == "" {
		return nil
	}

	args = append(args, marketID)
	_, err := Pool.Exec(ctx,
		fmt.Sprintf(`UPDATE markets SET %s WHERE id = $%d`, setClauses, i),
		args...,
	)
	return err
}

func GetActiveMarkets(ctx context.Context) ([]models.Market, error) {
	rows, err := Pool.Query(ctx,
		`SELECT `+marketColumns+` FROM markets WHERE status = 'active' ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	return scanMarkets(rows)
}

func GetResolvedMarkets(ctx context.Context) ([]models.Market, error) {
	rows, err := Pool.Query(ctx,
		`SELECT `+marketColumns+` FROM markets WHERE status = 'resolved' ORDER BY resolved_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	return scanMarkets(rows)
}

func GetMarketsByType(ctx context.Context, marketType models.MarketType) ([]models.Market, error) {
	rows, err := Pool.Query(ctx,
		`SELECT `+marketColumns+` FROM markets WHERE market_type = $1 ORDER BY created_at DESC`,
		string(marketType),
	)
	if err != nil {
		return nil, err
	}
	return scanMarkets(rows)
}

func GetMarketsByAsset(ctx context.Context, asset string) ([]models.Market, error) {
	rows, err := Pool.Query(ctx,
		`SELECT `+marketColumns+` FROM markets WHERE asset = $1 ORDER BY created_at DESC`,
		asset,
	)
	if err != nil {
		return nil, err
	}
	return scanMarkets(rows)
}

func GetActiveMarketsByType(ctx context.Context, marketType models.MarketType) ([]models.Market, error) {
	rows, err := Pool.Query(ctx,
		`SELECT `+marketColumns+` FROM markets WHERE status = 'active' AND market_type = $1 ORDER BY created_at DESC`,
		string(marketType),
	)
	if err != nil {
		return nil, err
	}
	return scanMarkets(rows)
}

func FindActiveCappmMarket(ctx context.Context, asset string, durationSeconds uint64) (*models.Market, error) {
	row := Pool.QueryRow(ctx, `
		SELECT `+marketColumns+`
		FROM markets
		WHERE market_type = 'CAPPM'
		  AND status = 'active'
		  AND asset = $1
		  AND duration_seconds = $2
		LIMIT 1
	`, asset, durationSeconds)

	m, err := scanMarket(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find active CAPPM market: %w", err)
	}
	return m, nil
}

type marketScanner interface {
	Scan(dest ...any) error
}

func scanMarket(row marketScanner) (*models.Market, error) {
	var m models.Market
	var marketType, status, direction, outcome string
	var resolvedAt *time.Time

	err := row.Scan(
		&m.ID, &marketType, &status, &m.QuoteCurrency, &m.Title,
		&m.Description, &m.DataProvider, &m.CreatorAddress, &m.MarketPDA,
		&m.StartTimeUTC, &m.EndTimeUTC, &m.DurationSeconds, &m.CreatedAt,
		&m.CreationTxHash, &m.Asset, &direction, &m.TargetPrice,
		&m.CurrentPrice, &outcome, &m.OutcomeDescription,
		&m.EndPrice, &m.SettlementTxHash, &resolvedAt,
	)
	if err != nil {
		return nil, err
	}

	m.MarketType = models.MarketType(marketType)
	m.Status = models.MarketStatus(status)
	m.Direction = models.MarketDirection(direction)
	m.Outcome = models.MarketOutcome(outcome)
	m.ResolvedAt = resolvedAt

	return &m, nil
}

func scanMarkets(rows pgx.Rows) ([]models.Market, error) {
	defer rows.Close()
	var markets []models.Market
	for rows.Next() {
		m, err := scanMarket(rows)
		if err != nil {
			log.Printf("[DB] Failed to scan market row: %v", err)
			continue
		}
		markets = append(markets, *m)
	}
	return markets, rows.Err()
}