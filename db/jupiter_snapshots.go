package db

import (
	"context"
	"time"
)

type MarketSnapshot struct {
	EventID     string    `json:"eventId"`
	MarketID    string    `json:"marketId"`
	MarketTitle string    `json:"marketTitle"`
	YesPrice    int64     `json:"yesPrice"` // micro-USD
	NoPrice     int64     `json:"noPrice"`
	RecordedAt  time.Time `json:"recordedAt"`
}

// InsertMarketSnapshot stores one price snapshot per market.
func InsertMarketSnapshot(ctx context.Context, eventID, marketID, title string, yesPrice, noPrice int64) error {
	_, err := Pool.Exec(ctx, `
		INSERT INTO jupiter_market_snapshots (event_id, market_id, market_title, yes_price, no_price, recorded_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
	`, eventID, marketID, title, yesPrice, noPrice)
	return err
}

// GetEventPriceHistory returns all snapshots for the given event, optionally
// limited to those recorded after `since`. Pass zero time for all history.
func GetEventPriceHistory(ctx context.Context, eventID string, since time.Time) ([]MarketSnapshot, error) {
	rows, err := Pool.Query(ctx, `
		SELECT event_id, market_id, market_title, yes_price, no_price, recorded_at
		FROM jupiter_market_snapshots
		WHERE event_id = $1
		  AND ($2::timestamptz IS NULL OR recorded_at >= $2)
		ORDER BY recorded_at ASC
	`, eventID, nullIfZero(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MarketSnapshot
	for rows.Next() {
		var s MarketSnapshot
		if err := rows.Scan(&s.EventID, &s.MarketID, &s.MarketTitle, &s.YesPrice, &s.NoPrice, &s.RecordedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// PruneEndedEventSnapshots deletes all snapshots for events that have received
// no new snapshot within idleFor — i.e. the match/event is over and we no longer
// need its price history. Returns the number of rows deleted. Active events keep
// getting fresh snapshots so they are never idle and never pruned.
func PruneEndedEventSnapshots(ctx context.Context, idleFor time.Duration) (int64, error) {
	cutoff := time.Now().Add(-idleFor)
	tag, err := Pool.Exec(ctx, `
		DELETE FROM jupiter_market_snapshots
		WHERE event_id IN (
			SELECT event_id FROM jupiter_market_snapshots
			GROUP BY event_id
			HAVING MAX(recorded_at) < $1
		)`, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func nullIfZero(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
