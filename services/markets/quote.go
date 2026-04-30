package markets

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/models"
	"github.com/vant-xyz/backend-code/services"
	"github.com/vant-xyz/backend-code/utils"
)

const quoteReservationTTL = 15 * time.Second

type ExecutableQuote struct {
	ID              string           `json:"id"`
	MarketID        string           `json:"market_id"`
	Side            models.OrderSide `json:"side"`
	Stake           float64          `json:"stake"`
	AvgPrice        float64          `json:"avg_price"`
	EstimatedShares float64          `json:"estimated_shares"`
	FillsCompletely bool             `json:"fills_completely"`
	TotalCost       float64          `json:"total_cost"`
	ExpiresAt       time.Time        `json:"expires_at"`
}

type AcceptQuoteInput struct {
	QuoteID   string
	UserEmail string
	MarketID  string
	IsDemo    bool
}

func CreateExecutableQuote(ctx context.Context, marketID, userEmail string, side models.OrderSide, stake float64) (*ExecutableQuote, error) {
	if stake <= 0 {
		return nil, fmt.Errorf("stake must be positive")
	}
	market, err := GetMarketByID(ctx, marketID)
	if err != nil {
		return nil, fmt.Errorf("market not found: %w", err)
	}
	if market.Status != models.MarketStatusActive {
		return nil, fmt.Errorf("market %s is not active", marketID)
	}
	reservation, err := GetMatchingEngine().ReserveQuote(marketID, userEmail, side, stake, quoteReservationTTL)
	if err != nil {
		return nil, err
	}
	if err := globalRiskState.canReserve(marketID, side, reservation.TotalCost, reservation.EstimatedShares); err != nil {
		GetMatchingEngine().ReleaseQuote(marketID, reservation.ID)
		return nil, err
	}
	globalRiskState.reserve(marketID, side, reservation.TotalCost, reservation.EstimatedShares)
	go func(quoteID, quoteMarketID string) {
		time.Sleep(quoteReservationTTL)
		GetMatchingEngine().ReleaseQuote(quoteMarketID, quoteID)
		globalRiskState.release(quoteMarketID, side, reservation.TotalCost, reservation.EstimatedShares)
	}(reservation.ID, reservation.MarketID)
	return &ExecutableQuote{
		ID:              reservation.ID,
		MarketID:        reservation.MarketID,
		Side:            reservation.Side,
		Stake:           reservation.Stake,
		AvgPrice:        reservation.AvgPrice,
		EstimatedShares: reservation.EstimatedShares,
		FillsCompletely: reservation.FillsCompletely,
		TotalCost:       reservation.TotalCost,
		ExpiresAt:       reservation.ExpiresAt,
	}, nil
}

func AcceptExecutableQuote(ctx context.Context, input AcceptQuoteInput) (*models.Order, *ExecutableQuote, error) {
	market, err := GetMarketByID(ctx, input.MarketID)
	if err != nil {
		return nil, nil, fmt.Errorf("market not found: %w", err)
	}
	if market.Status != models.MarketStatusActive {
		return nil, nil, fmt.Errorf("market %s is not active", input.MarketID)
	}

	engine := GetMatchingEngine()
	reservation, err := engine.GetQuote(input.MarketID, input.QuoteID)
	if err != nil {
		return nil, nil, err
	}
	if reservation.UserEmail != input.UserEmail {
		return nil, nil, fmt.Errorf("quote does not belong to user")
	}
	if !globalRiskState.allowAccept(input.UserEmail) {
		return nil, nil, fmt.Errorf("quote acceptance rate limit exceeded")
	}

	currency := orderBalanceCurrency(input.IsDemo)
	if err := services.LockBalance(ctx, input.UserEmail, reservation.TotalCost, currency); err != nil {
		return nil, nil, fmt.Errorf("insufficient balance: %w", err)
	}

	now := time.Now().UTC()
	order := &models.Order{
		ID:            fmt.Sprintf("ORD_%s", utils.RandomAlphanumeric(12)),
		UserEmail:     input.UserEmail,
		MarketID:      input.MarketID,
		Side:          reservation.Side,
		Type:          models.OrderTypeMarket,
		Price:         reservation.AvgPrice,
		Quantity:      reservation.EstimatedShares,
		FilledQty:     0,
		RemainingQty:  reservation.EstimatedShares,
		Status:        models.OrderStatusOpen,
		QuoteCurrency: market.QuoteCurrency,
		IsDemo:        input.IsDemo,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := db.RedisStoreOrder(ctx, order); err != nil {
		if unlockErr := services.UnlockBalance(ctx, input.UserEmail, reservation.TotalCost, currency); unlockErr != nil {
			log.Printf("[Quotes] CRITICAL: failed to unlock after order save failure for %s: %v", input.UserEmail, unlockErr)
		}
		return nil, nil, fmt.Errorf("failed to save order: %w", err)
	}
	db.AsyncSyncOrderToPG(order, func(c context.Context, o *models.Order) error {
		return db.SaveOrder(c, o)
	})

	acceptedReservation, err := engine.AcceptQuote(input.MarketID, input.QuoteID, order)
	if err != nil {
		if unlockErr := services.UnlockBalance(ctx, input.UserEmail, reservation.TotalCost, currency); unlockErr != nil {
			log.Printf("[Quotes] CRITICAL: failed to unlock after accept failure for %s: %v", input.UserEmail, unlockErr)
		}
		order.Status = models.OrderStatusCancelled
		order.RemainingQty = 0
		if redisErr := db.RedisUpdateOrderFill(ctx, order.ID, order.FilledQty, order.RemainingQty, order.Status); redisErr != nil {
			log.Printf("[Quotes] Failed to cancel rejected quote order %s in redis: %v", order.ID, redisErr)
		}
		db.AsyncSyncFillToPG(order)
		return nil, nil, err
	}
	globalRiskState.release(input.MarketID, acceptedReservation.Side, acceptedReservation.TotalCost, acceptedReservation.EstimatedShares)

	go persistOrderFill(order)

	return order, &ExecutableQuote{
		ID:              acceptedReservation.ID,
		MarketID:        acceptedReservation.MarketID,
		Side:            acceptedReservation.Side,
		Stake:           acceptedReservation.Stake,
		AvgPrice:        acceptedReservation.AvgPrice,
		EstimatedShares: acceptedReservation.EstimatedShares,
		FillsCompletely: acceptedReservation.FillsCompletely,
		TotalCost:       acceptedReservation.TotalCost,
		ExpiresAt:       acceptedReservation.ExpiresAt,
	}, nil
}
