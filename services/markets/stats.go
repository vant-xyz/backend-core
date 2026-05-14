package markets

import (
	"context"
	"math"
	"time"

	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/models"
)

type MarketStats struct {
	OpenInterest float64 `json:"open_interest"`
	Volatility   float64 `json:"volatility"`
	Volume3m     float64 `json:"volume_3m,omitempty"`
	Volume24h    float64 `json:"volume_24h,omitempty"`
}

type MarketHistoryEntry struct {
	ID         string `json:"id"`
	Outcome    string `json:"outcome"`
	EndTimeUTC string `json:"end_time_utc"`
}

func GetMarketStats(ctx context.Context, marketID string, marketType models.MarketType) (*MarketStats, error) {
	stats := &MarketStats{}

	if err := db.Pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(shares), 0)
		FROM positions
		WHERE market_id = $1 AND status = 'ACTIVE'
	`, marketID).Scan(&stats.OpenInterest); err != nil {
		return nil, err
	}

	rows, err := db.Pool.Query(ctx, `
		SELECT price FROM orders
		WHERE market_id = $1
		  AND status IN ('FILLED', 'PARTIALLY_FILLED')
		  AND price > 0
		ORDER BY updated_at ASC
		LIMIT 100
	`, marketID)
	if err == nil {
		var prices []float64
		for rows.Next() {
			var p float64
			if rows.Scan(&p) == nil && p > 0 {
				prices = append(prices, p)
			}
		}
		rows.Close()
		if len(prices) >= 2 {
			stats.Volatility = logReturnStdDev(prices)
		}
	}

	now := time.Now().UTC()
	if marketType == models.MarketTypeCAPPM {
		since := now.Add(-3 * time.Minute)
		db.Pool.QueryRow(ctx, `
			SELECT COALESCE(SUM(price * filled_qty), 0)
			FROM orders
			WHERE market_id = $1
			  AND status IN ('FILLED', 'PARTIALLY_FILLED')
			  AND updated_at >= $2
		`, marketID, since).Scan(&stats.Volume3m)
	} else {
		since := now.Add(-24 * time.Hour)
		db.Pool.QueryRow(ctx, `
			SELECT COALESCE(SUM(price * filled_qty), 0)
			FROM orders
			WHERE market_id = $1
			  AND status IN ('FILLED', 'PARTIALLY_FILLED')
			  AND updated_at >= $2
		`, marketID, since).Scan(&stats.Volume24h)
	}

	return stats, nil
}

func logReturnStdDev(prices []float64) float64 {
	n := len(prices)
	if n < 2 {
		return 0
	}
	returns := make([]float64, n-1)
	for i := 1; i < n; i++ {
		if prices[i-1] > 0 {
			returns[i-1] = math.Log(prices[i] / prices[i-1])
		}
	}
	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))
	variance := 0.0
	for _, r := range returns {
		d := r - mean
		variance += d * d
	}
	if len(returns) > 1 {
		variance /= float64(len(returns) - 1)
	}
	return math.Sqrt(variance) * 100
}

func GetMarketHistory(ctx context.Context, asset string, durationSeconds uint64, excludeID string) ([]MarketHistoryEntry, error) {
	since := time.Now().UTC().Add(-24 * time.Hour)
	rows, err := db.Pool.Query(ctx, `
		SELECT id, COALESCE(outcome, ''), end_time_utc
		FROM markets
		WHERE market_type = 'CAPPM'
		  AND asset = $1
		  AND duration_seconds = $2
		  AND status = 'resolved'
		  AND outcome IS NOT NULL AND outcome != ''
		  AND end_time_utc >= $3
		  AND id != $4
		ORDER BY end_time_utc DESC
		LIMIT 20
	`, asset, durationSeconds, since, excludeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []MarketHistoryEntry
	for rows.Next() {
		var e MarketHistoryEntry
		var endTime time.Time
		if err := rows.Scan(&e.ID, &e.Outcome, &endTime); err != nil {
			continue
		}
		e.EndTimeUTC = endTime.UTC().Format(time.RFC3339)
		entries = append(entries, e)
	}
	return entries, nil
}
