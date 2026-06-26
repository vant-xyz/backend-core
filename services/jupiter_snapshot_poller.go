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
const snapshotInterval = 10 * time.Minute

// endedEventIdle is how long an event can go without a new snapshot before we
// consider its match over and prune its history. Comfortably longer than the
// snapshot interval so a brief gap never drops an active event.
const endedEventIdle = 6 * time.Hour

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

	// Fetch the top 100 events by volume across every category.
	// Jupiter defaults to only 10 events; "start"/"end" are pagination slice
	// bounds (max range 100) — NOT timestamps. start=0&end=100 grabs the first
	// 100 events, which covers both the World Cup matches and general markets.
	// "category=all" is a valid enum value per Jupiter docs.
	// Market status is checked in code below.
	params := url.Values{
		"category":       {"all"},
		"includeMarkets": {"true"},
		"sortBy":         {"volume"},
		"start":          {"0"},
		"end":            {"100"},
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

		// Prune history for ended matches. Gated on a successful save so a broken
		// or paused poller can never wipe data (every event would look "idle").
		if pruned, err := db.PruneEndedEventSnapshots(ctx, endedEventIdle); err != nil {
			log.Printf("[JupiterPoller] prune error: %v", err)
		} else if pruned > 0 {
			log.Printf("[JupiterPoller] Pruned %d snapshots for ended matches", pruned)
		}
	}
}
