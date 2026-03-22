package markets

import (
	"context"
	"fmt"
	"log"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/models"
	"github.com/vant-xyz/backend-code/services"
	"github.com/vant-xyz/backend-code/utils"
)

const settlementsCollection = "settlement_payouts"

type Payout struct {
	ID            string    `json:"id" firestore:"id"`
	MarketID      string    `json:"market_id" firestore:"market_id"`
	PositionID    string    `json:"position_id" firestore:"position_id"`
	UserEmail     string    `json:"user_email" firestore:"user_email"`
	Shares        float64   `json:"shares" firestore:"shares"`
	PayoutAmount  float64   `json:"payout_amount" firestore:"payout_amount"`
	QuoteCurrency string    `json:"quote_currency" firestore:"quote_currency"`
	Processed     bool      `json:"processed" firestore:"processed"`
	CreatedAt     time.Time `json:"created_at" firestore:"created_at"`
	ProcessedAt   *time.Time `json:"processed_at,omitempty" firestore:"processed_at"`
}

type MarketSettlementResult struct {
	MarketID       string    `json:"market_id"`
	Outcome        string    `json:"outcome"`
	TotalPositions int       `json:"total_positions"`
	WinningCount   int       `json:"winning_count"`
	LosingCount    int       `json:"losing_count"`
	TotalPayout    float64   `json:"total_payout"`
	RefundedOrders int       `json:"refunded_orders"`
	SettledAt      time.Time `json:"settled_at"`
}

// ProcessMarketSettlement is the entry point for settling a market.
// It is fully idempotent — safe to call multiple times for the same market.
// Sequence:
//   1. Validate market is resolved onchain
//   2. Calculate payouts for all active positions
//   3. Distribute payouts (idempotent per position via payout record)
//   4. Refund all open orders
//   5. Close matching engine goroutine
//   6. Broadcast settlement event to all subscribers
func ProcessMarketSettlement(ctx context.Context, marketID string, outcome models.MarketOutcome) (*MarketSettlementResult, error) {
	market, err := GetMarketByID(ctx, marketID)
	if err != nil {
		return nil, fmt.Errorf("market not found: %w", err)
	}

	if market.Status != models.MarketStatusResolved {
		return nil, fmt.Errorf("market %s is not resolved yet", marketID)
	}

	log.Printf("[Settlement] Starting settlement: market=%s outcome=%s", marketID, outcome)

	payouts, err := CalculatePayouts(ctx, marketID, outcome, market.QuoteCurrency)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate payouts: %w", err)
	}

	totalPayout, winCount, loseCount, err := DistributePayouts(ctx, payouts)
	if err != nil {
		return nil, fmt.Errorf("failed to distribute payouts: %w", err)
	}

	refundedCount, err := RefundOpenOrders(ctx, marketID)
	if err != nil {
		log.Printf("[Settlement] Warning: partial refund failure for market %s: %v", marketID, err)
	}

	GetMatchingEngine().CloseMarket(marketID)

	result := &MarketSettlementResult{
		MarketID:       marketID,
		Outcome:        string(outcome),
		TotalPositions: len(payouts),
		WinningCount:   winCount,
		LosingCount:    loseCount,
		TotalPayout:    totalPayout,
		RefundedOrders: refundedCount,
		SettledAt:      time.Now(),
	}

	BroadcastOrderbookUpdate(marketID, "SETTLEMENT", map[string]interface{}{
		"outcome":      string(outcome),
		"total_payout": totalPayout,
		"settled_at":   result.SettledAt,
	})

	log.Printf("[Settlement] Complete: market=%s outcome=%s winners=%d losers=%d payout=%.2f refunds=%d",
		marketID, outcome, winCount, loseCount, totalPayout, refundedCount)

	return result, nil
}

// CalculatePayouts builds payout records for every active position in a market.
// Records are written to Firestore before distribution so we can resume if the
// process crashes halfway through.
func CalculatePayouts(ctx context.Context, marketID string, outcome models.MarketOutcome, quoteCurrency string) ([]Payout, error) {
	positions, err := GetMarketPositions(ctx, marketID, models.PositionStatusActive)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch active positions: %w", err)
	}

	if len(positions) == 0 {
		log.Printf("[Settlement] No active positions for market %s", marketID)
		return nil, nil
	}

	payouts := make([]Payout, 0, len(positions))
	now := time.Now()

	batch := db.Client.Batch()
	batchCount := 0

	for _, pos := range positions {
		existing, err := getExistingPayout(ctx, marketID, pos.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to check existing payout for position %s: %w", pos.ID, err)
		}
		if existing != nil {
			payouts = append(payouts, *existing)
			continue
		}

		won := (pos.Side == models.OrderSideYes && outcome == models.OutcomeYes) ||
			(pos.Side == models.OrderSideNo && outcome == models.OutcomeNo)

		payoutAmount := 0.0
		if won {
			payoutAmount = pos.Shares * payoutPerWinningShare
		}

		payout := Payout{
			ID:            fmt.Sprintf("PAY_%s", utils.RandomAlphanumeric(12)),
			MarketID:      marketID,
			PositionID:    pos.ID,
			UserEmail:     pos.UserEmail,
			Shares:        pos.Shares,
			PayoutAmount:  payoutAmount,
			QuoteCurrency: quoteCurrency,
			Processed:     false,
			CreatedAt:     now,
		}

		docRef := db.Client.Collection(settlementsCollection).Doc(payout.ID)
		batch.Set(docRef, payout)
		batchCount++

		payouts = append(payouts, payout)

		if batchCount == 400 {
			if _, err := batch.Commit(ctx); err != nil {
				return nil, fmt.Errorf("failed to commit payout batch: %w", err)
			}
			batch = db.Client.Batch()
			batchCount = 0
		}
	}

	if batchCount > 0 {
		if _, err := batch.Commit(ctx); err != nil {
			return nil, fmt.Errorf("failed to commit final payout batch: %w", err)
		}
	}

	log.Printf("[Settlement] Calculated %d payouts for market %s", len(payouts), marketID)
	return payouts, nil
}

// DistributePayouts processes each payout record — credits winners and marks
// all positions settled. Skips already-processed payouts (idempotent).
func DistributePayouts(ctx context.Context, payouts []Payout) (totalPayout float64, winCount, loseCount int, err error) {
	for i := range payouts {
		payout := &payouts[i]

		if payout.Processed {
			if payout.PayoutAmount > 0 {
				winCount++
				totalPayout += payout.PayoutAmount
			} else {
				loseCount++
			}
			continue
		}

		if err := SettlePosition(ctx, payout.PositionID, outcomeFromPayout(payout)); err != nil {
			log.Printf("[Settlement] CRITICAL: failed to settle position %s for user %s: %v",
				payout.PositionID, payout.UserEmail, err)
			continue
		}

		if payout.PayoutAmount > 0 {
			if err := services.CreditBalance(ctx, payout.UserEmail, payout.PayoutAmount, payout.QuoteCurrency); err != nil {
				log.Printf("[Settlement] CRITICAL: failed to credit %.2f to %s for payout %s: %v",
					payout.PayoutAmount, payout.UserEmail, payout.ID, err)
				continue
			}
			winCount++
			totalPayout += payout.PayoutAmount

			GetOrderbookHub().BroadcastToUser(payout.UserEmail, OrderbookUpdate{
				MarketID: payout.MarketID,
				Type:     "PAYOUT",
				Data: map[string]interface{}{
					"payout_amount":  payout.PayoutAmount,
					"quote_currency": payout.QuoteCurrency,
					"position_id":    payout.PositionID,
				},
			})
		} else {
			loseCount++
		}

		now := time.Now()
		if _, err := db.Client.Collection(settlementsCollection).Doc(payout.ID).Update(ctx, []firestore.Update{
			{Path: "processed", Value: true},
			{Path: "processed_at", Value: now},
		}); err != nil {
			log.Printf("[Settlement] Warning: failed to mark payout %s as processed: %v", payout.ID, err)
		}
	}

	return totalPayout, winCount, loseCount, nil
}

// RefundOpenOrders cancels all open/partially-filled orders for a market
// and unlocks their reserved funds. Safe to call multiple times — CancelOrder
// is a no-op on already-cancelled orders.
func RefundOpenOrders(ctx context.Context, marketID string) (int, error) {
	openOrders, err := GetOpenOrdersForMarket(ctx, marketID)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch open orders for market %s: %w", marketID, err)
	}

	refundedCount := 0
	for _, order := range openOrders {
		if err := CancelOrder(ctx, order.ID, order.UserEmail); err != nil {
			log.Printf("[Settlement] Failed to refund order %s for user %s: %v",
				order.ID, order.UserEmail, err)
			continue
		}
		refundedCount++
	}

	log.Printf("[Settlement] Refunded %d open orders for market %s", refundedCount, marketID)
	return refundedCount, nil
}

func getExistingPayout(ctx context.Context, marketID, positionID string) (*Payout, error) {
	iter := db.Client.Collection(settlementsCollection).
		Where("market_id", "==", marketID).
		Where("position_id", "==", positionID).
		Limit(1).
		Documents(ctx)

	doc, err := iter.Next()
	if err != nil {
		return nil, nil
	}

	var payout Payout
	if err := doc.DataTo(&payout); err != nil {
		return nil, fmt.Errorf("failed to deserialize payout: %w", err)
	}
	return &payout, nil
}

// outcomeFromPayout reconstructs the market outcome from whether the payout
// was non-zero. Used when re-processing already-calculated payouts.
func outcomeFromPayout(p *Payout) models.MarketOutcome {
	if p.PayoutAmount > 0 {
		return models.OutcomeYes
	}
	return models.OutcomeNo
}