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
	"google.golang.org/api/iterator"
)

const (
	positionsCollection  = "positions"
	payoutPerWinningShare = 100.0
)

type UpsertPositionInput struct {
	UserEmail     string
	MarketID      string
	Side          models.OrderSide
	Shares        float64
	FillPrice     float64
	QuoteCurrency string
}

// UpsertPosition creates a new position or adds shares to an existing one,
// recalculating the volume-weighted average entry price on each fill.
// Called by the matching engine on every successful order match.
func UpsertPosition(ctx context.Context, input UpsertPositionInput) (*models.Position, error) {
	existing, err := getUserPositionForMarketSide(ctx, input.UserEmail, input.MarketID, input.Side)
	if err != nil {
		return nil, fmt.Errorf("failed to query existing position: %w", err)
	}

	now := time.Now()

	if existing != nil {
		totalShares := existing.Shares + input.Shares
		newAvgEntry := ((existing.Shares * existing.AvgEntryPrice) + (input.Shares * input.FillPrice)) / totalShares

		updates := []firestore.Update{
			{Path: "shares", Value: totalShares},
			{Path: "avg_entry_price", Value: newAvgEntry},
			{Path: "updated_at", Value: now},
		}

		if _, err := db.Client.Collection(positionsCollection).Doc(existing.ID).Update(ctx, updates); err != nil {
			return nil, fmt.Errorf("failed to update position %s: %w", existing.ID, err)
		}

		existing.Shares = totalShares
		existing.AvgEntryPrice = newAvgEntry
		existing.UpdatedAt = now
		return existing, nil
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

	if _, err := db.Client.Collection(positionsCollection).Doc(position.ID).Set(ctx, position); err != nil {
		return nil, fmt.Errorf("failed to save position: %w", err)
	}

	return position, nil
}

// SettlePosition resolves a position against a market outcome.
// Winners receive payoutPerWinningShare per share credited to their balance.
// Losers receive nothing. Both are marked SETTLED.
func SettlePosition(ctx context.Context, positionID string, outcome models.MarketOutcome) error {
	doc, err := db.Client.Collection(positionsCollection).Doc(positionID).Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch position %s: %w", positionID, err)
	}

	var position models.Position
	if err := doc.DataTo(&position); err != nil {
		return fmt.Errorf("failed to deserialize position %s: %w", positionID, err)
	}

	if position.Status == models.PositionStatusSettled {
		return nil
	}

	now := time.Now()
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

	updates := []firestore.Update{
		{Path: "status", Value: string(models.PositionStatusSettled)},
		{Path: "payout_amount", Value: payout},
		{Path: "realized_pnl", Value: realizedPnL},
		{Path: "settled_at", Value: now},
		{Path: "updated_at", Value: now},
	}

	if _, err := db.Client.Collection(positionsCollection).Doc(positionID).Update(ctx, updates); err != nil {
		return fmt.Errorf("failed to mark position %s as settled: %w", positionID, err)
	}

	log.Printf("[Positions] Settled %s: user=%s side=%s shares=%.2f payout=%.2f pnl=%.2f",
		positionID, position.UserEmail, position.Side, position.Shares, payout, realizedPnL)

	return nil
}

// GetUserPositions returns all positions for a user, optionally filtered by market.
func GetUserPositions(ctx context.Context, userEmail string, marketID string) ([]models.Position, error) {
	var q firestore.Query

	if marketID != "" {
		q = db.Client.Collection(positionsCollection).
			Where("user_email", "==", userEmail).
			Where("market_id", "==", marketID).
			OrderBy("created_at", firestore.Desc)
	} else {
		q = db.Client.Collection(positionsCollection).
			Where("user_email", "==", userEmail).
			OrderBy("created_at", firestore.Desc)
	}

	return queryPositions(ctx, q)
}

// GetMarketPositions returns all positions in a market, optionally filtered by status.
// Used by the settlement service to find all positions to pay out.
func GetMarketPositions(ctx context.Context, marketID string, status models.PositionStatus) ([]models.Position, error) {
	q := db.Client.Collection(positionsCollection).
		Where("market_id", "==", marketID).
		Where("status", "==", string(status))

	return queryPositions(ctx, q)
}

// GetPositionValue returns the current estimated value of a position based on
// the last traded price. This is an unrealised value — not a guaranteed payout.
func GetPositionValue(ctx context.Context, position *models.Position) (float64, error) {
	engine := GetMatchingEngine()
	lastPrice := engine.GetLastTradedPrice(position.MarketID)

	if lastPrice == 0 {
		return position.Shares * position.AvgEntryPrice, nil
	}

	return position.Shares * lastPrice, nil
}

func getUserPositionForMarketSide(ctx context.Context, userEmail, marketID string, side models.OrderSide) (*models.Position, error) {
	iter := db.Client.Collection(positionsCollection).
		Where("user_email", "==", userEmail).
		Where("market_id", "==", marketID).
		Where("side", "==", string(side)).
		Where("status", "==", string(models.PositionStatusActive)).
		Limit(1).
		Documents(ctx)

	doc, err := iter.Next()
	if err == iterator.Done {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("firestore query failed: %w", err)
	}

	var position models.Position
	if err := doc.DataTo(&position); err != nil {
		return nil, fmt.Errorf("failed to deserialize position: %w", err)
	}
	return &position, nil
}

func queryPositions(ctx context.Context, q firestore.Query) ([]models.Position, error) {
	iter := q.Documents(ctx)
	var positions []models.Position

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error iterating positions: %w", err)
		}
		var position models.Position
		if err := doc.DataTo(&position); err != nil {
			log.Printf("[Positions] Failed to deserialize position %s: %v", doc.Ref.ID, err)
			continue
		}
		positions = append(positions, position)
	}

	return positions, nil
}