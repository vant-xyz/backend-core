package services

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"time"

	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/services/jupiter"
)

// snapshotInterval controls how often we capture price snapshots.
const snapshotInterval = 15 * time.Minute

type jupMarketPricing struct {
	BuyYesPriceUsd int64 `json:"buyYesPriceUsd"`
	BuyNoPriceUsd  int64 `json:"buyNoPriceUsd"`
}

type jupMarket struct {
	MarketID string            `json:"marketId"`
	EventID  string            `json:"eventId"`
	Title    string            `json:"title"`
	Status   string            `json:"status"`
	Pricing  *jupMarketPricing `json:"pricing"`
}

type jupEvent struct {
	EventID string      `json:"eventId"`
	Markets []jupMarket `json:"markets"`
}

type jupEventsResponse struct {
	Data []jupEvent `json:"data"`
}

// StartJupiterSnapshotPoller begins a background loop that periodically
// fetches active event markets from Jupiter and stores price snapshots.
func StartJupiterSnapshotPoller() {
	go func() {
		// Immediate first run so we don't wait 15 min after cold-start.
		runSnapshot()

		ticker := time.NewTicker(snapshotInterval)
		defer ticker.Stop()
		for range ticker.C {
			runSnapshot()
		}
	}()
	log.Println("[JupiterPoller] Started — snapshotting every", snapshotInterval)
}

func runSnapshot() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Fetch all open events across every category, enough to cover general markets.
	// No "filter" param: Jupiter treats it as a text search, not a status filter.
	// Market status is checked in code below.
	params := url.Values{
		"includeMarkets": {"true"},
		"sortBy":         {"volume"},
		"limit":          {"200"},
	}
	raw, status, err := jupiter.Get(ctx, "/events", params)
	if err != nil || status != 200 {
		log.Printf("[JupiterPoller] fetch events error (status=%d): %v", status, err)
		return
	}

	var resp jupEventsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		log.Printf("[JupiterPoller] unmarshal error: %v", err)
		return
	}

	saved := 0
	for _, event := range resp.Data {
		for _, m := range event.Markets {
			if m.Status != "open" || m.Pricing == nil {
				continue
			}
			if err := db.InsertMarketSnapshot(
				ctx,
				event.EventID,
				m.MarketID,
				m.Title,
				m.Pricing.BuyYesPriceUsd,
				m.Pricing.BuyNoPriceUsd,
			); err != nil {
				log.Printf("[JupiterPoller] insert snapshot error (market=%s): %v", m.MarketID, err)
			} else {
				saved++
			}
		}
	}

	if saved > 0 {
		log.Printf("[JupiterPoller] Saved %d market snapshots at %s", saved, time.Now().Format(time.RFC3339))
	}
}
