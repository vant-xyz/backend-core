package markets

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/models"
	"github.com/vant-xyz/backend-code/utils"
)

type Payout struct {
	ID            string     `json:"id"`
	MarketID      string     `json:"market_id"`
	PositionID    string     `json:"position_id"`
	UserEmail     string     `json:"user_email"`
	Shares        float64    `json:"shares"`
	PayoutAmount  float64    `json:"payout_amount"`
	QuoteCurrency string     `json:"quote_currency"`
	Processed     bool       `json:"processed"`
	CreatedAt     time.Time  `json:"created_at"`
	ProcessedAt   *time.Time `json:"processed_at,omitempty"`
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

func ProcessMarketSettlement(ctx context.Context, marketID string, outcome models.MarketOutcome) (*MarketSettlementResult, error) {
	market, err := GetMarketByID(ctx, marketID)
	if err != nil {
		return nil, fmt.Errorf("market not found: %w", err)
	}
	if market.Status != models.MarketStatusResolved {
		return nil, fmt.Errorf("market %s is not resolved yet", marketID)
	}

	log.Printf("[Settlement] Starting: market=%s outcome=%s", marketID, outcome)

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

func CalculatePayouts(ctx context.Context, marketID string, outcome models.MarketOutcome, quoteCurrency string) ([]Payout, error) {
	positions, err := GetMarketPositions(ctx, marketID, models.PositionStatusActive)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch active positions: %w", err)
	}
	if len(positions) == 0 {
		log.Printf("[Settlement] No active positions for market %s", marketID)
		return nil, nil
	}

	now := time.Now()
	payouts := make([]Payout, 0, len(positions))
	batch := make([]db.PayoutRecord, 0, len(positions))

	for _, pos := range positions {
		existing, err := db.GetExistingPayout(ctx, marketID, pos.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to check existing payout for position %s: %w", pos.ID, err)
		}
		if existing != nil {
			payouts = append(payouts, Payout{
				ID:            existing.ID,
				MarketID:      existing.MarketID,
				PositionID:    existing.PositionID,
				UserEmail:     existing.UserEmail,
				Shares:        existing.Shares,
				PayoutAmount:  existing.PayoutAmount,
				QuoteCurrency: existing.QuoteCurrency,
				Processed:     existing.Processed,
				CreatedAt:     now,
			})
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

		batch = append(batch, db.PayoutRecord{
			ID:            payout.ID,
			MarketID:      payout.MarketID,
			PositionID:    payout.PositionID,
			UserEmail:     payout.UserEmail,
			Shares:        payout.Shares,
			PayoutAmount:  payout.PayoutAmount,
			QuoteCurrency: payout.QuoteCurrency,
			Processed:     false,
		})
		payouts = append(payouts, payout)
	}

	if len(batch) > 0 {
		if err := db.SavePayoutRecordsBatch(ctx, batch); err != nil {
			return nil, fmt.Errorf("failed to save payout records: %w", err)
		}
	}

	log.Printf("[Settlement] Calculated %d payouts for market %s", len(payouts), marketID)
	return payouts, nil
}

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

		outcomeForPosition := models.OutcomeNo
		if payout.PayoutAmount > 0 {
			outcomeForPosition = models.OutcomeYes
		}

		if err := SettlePosition(ctx, payout.PositionID, outcomeForPosition); err != nil {
			log.Printf("[Settlement] CRITICAL: failed to settle position %s for user %s: %v",
				payout.PositionID, payout.UserEmail, err)
			continue
		}

		if payout.PayoutAmount > 0 {
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
			go func(p Payout) {
				ppCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				wallet, err := db.GetWalletByEmail(ppCtx, p.UserEmail)
				if err != nil || wallet.SolPublicKey == "" {
					return
				}
				settlerKey, err := getSettlerKeypair()
				if err != nil {
					log.Printf("[MagicBlock] settler keypair unavailable for private payment: %v", err)
					return
				}
				units := uint64(p.PayoutAmount * 1_000_000)
				sig, err := SendPrivatePayment(ppCtx, settlerKey, wallet.SolPublicKey, units)
				if err != nil {
					log.Printf("[MagicBlock] private payment failed for %s: %v", p.UserEmail, err)
					return
				}
				log.Printf("[MagicBlock] private payment sent to %s sig=%s", p.UserEmail, sig)
			}(*payout)
		} else {
			loseCount++
		}

		if err := db.MarkPayoutProcessed(ctx, payout.ID); err != nil {
			log.Printf("[Settlement] Warning: failed to mark payout %s as processed: %v", payout.ID, err)
		}
	}
	return totalPayout, winCount, loseCount, nil
}

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