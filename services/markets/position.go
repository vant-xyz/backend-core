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

const payoutPerWinningShare = 1.0

type UpsertPositionInput struct {
	UserEmail     string
	MarketID      string
	Side          models.OrderSide
	Shares        float64
	FillPrice     float64
	QuoteCurrency string
}

func UpsertPosition(ctx context.Context, input UpsertPositionInput) (*models.Position, error) {
	existing, err := db.GetUserPositionForMarketSide(ctx, input.UserEmail, input.MarketID, input.Side)
	if err != nil {
		return nil, fmt.Errorf("failed to query existing position: %w", err)
	}

	now := time.Now()

	if existing != nil {
		return updateExistingPosition(ctx, existing, input.Shares, input.FillPrice, now)
	}

	position := &models.Position{
		ID:            fmt.Sprintf("POS_%s", utils.RandomAlphanumeric(12)),
		UserEmail:     input.UserEmail,
		MarketID:      input.MarketID,
		Side:          input.Side,
		Shares:        input.Shares,
		AvgEntryPrice: input.FillPrice,
		RealizedPnL:   0,
		PayoutAmount:  0,
		Status:        models.PositionStatusActive,
		QuoteCurrency: input.QuoteCurrency,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := db.SavePosition(ctx, position); err != nil {
		if db.IsDuplicateKeyError(err) {
			// Concurrent fill beat us to the insert — re-fetch and update.
			existing, err = db.GetUserPositionForMarketSide(ctx, input.UserEmail, input.MarketID, input.Side)
			if err != nil || existing == nil {
				return nil, fmt.Errorf("failed to recover from concurrent position insert: %w", err)
			}
			return updateExistingPosition(ctx, existing, input.Shares, input.FillPrice, now)
		}
		return nil, fmt.Errorf("failed to save position: %w", err)
	}
	return position, nil
}

func updateExistingPosition(ctx context.Context, existing *models.Position, newShares, fillPrice float64, now time.Time) (*models.Position, error) {
	totalShares := existing.Shares + newShares
	newAvgEntry := ((existing.Shares * existing.AvgEntryPrice) + (newShares * fillPrice)) / totalShares
	if err := db.UpdatePosition(ctx, existing.ID, totalShares, newAvgEntry); err != nil {
		return nil, fmt.Errorf("failed to update position %s: %w", existing.ID, err)
	}
	existing.Shares = totalShares
	existing.AvgEntryPrice = newAvgEntry
	existing.UpdatedAt = now
	return existing, nil
}

func SettlePosition(ctx context.Context, positionID string, outcome models.MarketOutcome) error {
	position, err := db.GetPositionByID(ctx, positionID)
	if err != nil {
		return fmt.Errorf("failed to fetch position %s: %w", positionID, err)
	}
	if position.Status == models.PositionStatusSettled {
		return nil
	}

	payout := 0.0
	realizedPnL := 0.0
	positionWon := (position.Side == models.OrderSideYes && outcome == models.OutcomeYes) ||
		(position.Side == models.OrderSideNo && outcome == models.OutcomeNo)

	if positionWon {
		payout = position.Shares * payoutPerWinningShare
		realizedPnL = payout - (position.Shares * position.AvgEntryPrice)
		if err := services.CreditBalance(ctx, position.UserEmail, payout, position.QuoteCurrency); err != nil {
			return fmt.Errorf("failed to credit payout for position %s user %s: %w",
				positionID, position.UserEmail, err)
		}
	}

	if err := db.SettlePositionRecord(ctx, positionID, payout, realizedPnL); err != nil {
		return fmt.Errorf("failed to mark position %s as settled: %w", positionID, err)
	}

	log.Printf("[Positions] Settled %s: user=%s side=%s shares=%.2f payout=%.2f pnl=%.2f",
		positionID, position.UserEmail, position.Side, position.Shares, payout, realizedPnL)
	return nil
}

func GetUserPositions(ctx context.Context, userEmail, marketID string) ([]models.Position, error) {
	return db.GetUserPositions(ctx, userEmail, marketID)
}

func GetMarketPositions(ctx context.Context, marketID string, status models.PositionStatus) ([]models.Position, error) {
	return db.GetMarketPositions(ctx, marketID, status)
}

func GetPositionValue(ctx context.Context, position *models.Position) (float64, error) {
	lastPrice := GetMatchingEngine().GetLastTradedPrice(position.MarketID)
	if lastPrice == 0 {
		return position.Shares * position.AvgEntryPrice, nil
	}
	return position.Shares * lastPrice, nil
}