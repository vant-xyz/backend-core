package markets

import (
	"context"
	"fmt"
	"log"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/models"
	"github.com/vant-xyz/backend-code/utils"
	"google.golang.org/api/iterator"
)

const (
	marketsCollection = "markets"
	defaultCurrency   = "NGN"
)

type CreateCAPPMInput struct {
	Title           string
	Description     string
	Asset           string
	Direction       models.MarketDirection
	TargetPrice     uint64
	CurrentPrice    uint64
	DataProvider    string
	StartTimeUTC    time.Time
	DurationSeconds uint64
}

type CreateGEMInput struct {
	Title           string
	Description     string
	DataProvider    string
	StartTimeUTC    time.Time
	DurationSeconds uint64
}

type SettleGEMInput struct {
	MarketID           string
	Outcome            models.MarketOutcome
	OutcomeDescription string
}

func CreateCAPPM(ctx context.Context, input CreateCAPPMInput) (*models.Market, error) {
	marketID := fmt.Sprintf("CAPPM_%s", utils.RandomAlphanumeric(10))

	settlerKey, err := getSettlerKeypair()
	if err != nil {
		return nil, err
	}

	marketPDA, _, err := deriveMarketPDA(marketID)
	if err != nil {
		return nil, fmt.Errorf("failed to derive market PDA: %w", err)
	}

	directionByte := uint8(0)
	if input.Direction == models.DirectionBelow {
		directionByte = 1
	}

	txHash, err := CreateMarketCAPPM(CreateMarketCAPPMParams{
		MarketID:        marketID,
		Title:           input.Title,
		Description:     input.Description,
		StartTimeUTC:    uint64(input.StartTimeUTC.Unix()),
		DurationSeconds: input.DurationSeconds,
		Direction:       directionByte,
		TargetPrice:     input.TargetPrice,
		DataProvider:    input.DataProvider,
		CurrentPrice:    input.CurrentPrice,
		Asset:           input.Asset,
	})
	if err != nil {
		return nil, fmt.Errorf("CreateMarketCAPPM onchain failed: %w", err)
	}

	now := time.Now()
	endTime := input.StartTimeUTC.Add(time.Duration(input.DurationSeconds) * time.Second)

	market := &models.Market{
		ID:              marketID,
		MarketType:      models.MarketTypeCAPPM,
		Status:          models.MarketStatusActive,
		QuoteCurrency:   defaultCurrency,
		Title:           input.Title,
		Description:     input.Description,
		DataProvider:    input.DataProvider,
		CreatorAddress:  settlerKey.PublicKey().String(),
		MarketPDA:       marketPDA.String(),
		StartTimeUTC:    input.StartTimeUTC,
		EndTimeUTC:      endTime,
		DurationSeconds: input.DurationSeconds,
		CreatedAt:       now,
		CreationTxHash:  txHash,
		Asset:           input.Asset,
		Direction:       input.Direction,
		TargetPrice:     input.TargetPrice,
		CurrentPrice:    input.CurrentPrice,
	}

	if err := saveMarket(ctx, market); err != nil {
		return nil, fmt.Errorf("failed to save CAPPM market to Firestore: %w", err)
	}

	spawnSettlementTimer(market)

	log.Printf("[Markets] CreateCAPPM complete: id=%s pda=%s tx=%s", marketID, marketPDA, txHash)
	return market, nil
}

func CreateGEM(ctx context.Context, input CreateGEMInput) (*models.Market, error) {
	marketID := fmt.Sprintf("GEM_%s", utils.RandomAlphanumeric(10))

	settlerKey, err := getSettlerKeypair()
	if err != nil {
		return nil, err
	}

	marketPDA, _, err := deriveMarketPDA(marketID)
	if err != nil {
		return nil, fmt.Errorf("failed to derive market PDA: %w", err)
	}

	txHash, err := CreateMarketGEM(CreateMarketGEMParams{
		MarketID:        marketID,
		Title:           input.Title,
		Description:     input.Description,
		StartTimeUTC:    uint64(input.StartTimeUTC.Unix()),
		DurationSeconds: input.DurationSeconds,
		DataProvider:    input.DataProvider,
	})
	if err != nil {
		return nil, fmt.Errorf("CreateMarketGEM onchain failed: %w", err)
	}

	now := time.Now()
	endTime := input.StartTimeUTC.Add(time.Duration(input.DurationSeconds) * time.Second)

	market := &models.Market{
		ID:              marketID,
		MarketType:      models.MarketTypeGEM,
		Status:          models.MarketStatusActive,
		QuoteCurrency:   defaultCurrency,
		Title:           input.Title,
		Description:     input.Description,
		DataProvider:    input.DataProvider,
		CreatorAddress:  settlerKey.PublicKey().String(),
		MarketPDA:       marketPDA.String(),
		StartTimeUTC:    input.StartTimeUTC,
		EndTimeUTC:      endTime,
		DurationSeconds: input.DurationSeconds,
		CreatedAt:       now,
		CreationTxHash:  txHash,
	}

	if err := saveMarket(ctx, market); err != nil {
		return nil, fmt.Errorf("failed to save GEM market to Firestore: %w", err)
	}

	log.Printf("[Markets] CreateGEM complete: id=%s pda=%s tx=%s", marketID, marketPDA, txHash)
	return market, nil
}

func SettleCAPPM(ctx context.Context, marketID string, endPriceCents uint64) error {
	market, err := GetMarketByID(ctx, marketID)
	if err != nil {
		return fmt.Errorf("market not found: %w", err)
	}

	if market.Status == models.MarketStatusResolved {
		return fmt.Errorf("market %s is already resolved", marketID)
	}

	if market.MarketType != models.MarketTypeCAPPM {
		return fmt.Errorf("market %s is not a CAPPM market", marketID)
	}

	txHash, err := SettleMarketCAPPM(marketID, endPriceCents)
	if err != nil {
		return fmt.Errorf("SettleMarketCAPPM onchain failed: %w", err)
	}

	outcome := resolveCAPPMOutcome(market.Direction, market.TargetPrice, endPriceCents)

	dollars := endPriceCents / 100
	cents := endPriceCents % 100
	outcomeDescription := fmt.Sprintf(
		"%s closed at $%d.%02d on %s",
		market.Asset, dollars, cents, market.DataProvider,
	)

	now := time.Now()
	updates := []firestore.Update{
		{Path: "status", Value: string(models.MarketStatusResolved)},
		{Path: "outcome", Value: string(outcome)},
		{Path: "outcome_description", Value: outcomeDescription},
		{Path: "end_price", Value: endPriceCents},
		{Path: "settlement_tx_hash", Value: txHash},
		{Path: "resolved_at", Value: now},
	}

	if err := updateMarket(ctx, marketID, updates); err != nil {
		return fmt.Errorf("failed to update CAPPM market in Firestore after settlement: %w", err)
	}

	log.Printf("[Markets] SettleCAPPM complete: id=%s outcome=%s endPrice=%d tx=%s",
		marketID, outcome, endPriceCents, txHash)
	return nil
}

func SettleGEM(ctx context.Context, input SettleGEMInput) error {
	market, err := GetMarketByID(ctx, input.MarketID)
	if err != nil {
		return fmt.Errorf("market not found: %w", err)
	}

	if market.Status == models.MarketStatusResolved {
		return fmt.Errorf("market %s is already resolved", input.MarketID)
	}

	if market.MarketType != models.MarketTypeGEM {
		return fmt.Errorf("market %s is not a GEM market", input.MarketID)
	}

	outcomeByte := uint8(0)
	if input.Outcome == models.OutcomeNo {
		outcomeByte = 1
	}

	txHash, err := SettleMarketGEM(input.MarketID, outcomeByte, input.OutcomeDescription)
	if err != nil {
		return fmt.Errorf("SettleMarketGEM onchain failed: %w", err)
	}

	now := time.Now()
	updates := []firestore.Update{
		{Path: "status", Value: string(models.MarketStatusResolved)},
		{Path: "outcome", Value: string(input.Outcome)},
		{Path: "outcome_description", Value: input.OutcomeDescription},
		{Path: "settlement_tx_hash", Value: txHash},
		{Path: "resolved_at", Value: now},
	}

	if err := updateMarket(ctx, input.MarketID, updates); err != nil {
		return fmt.Errorf("failed to update GEM market in Firestore after settlement: %w", err)
	}

	log.Printf("[Markets] SettleGEM complete: id=%s outcome=%s tx=%s",
		input.MarketID, input.Outcome, txHash)
	return nil
}

func SyncMarketFromChain(ctx context.Context, marketID string) (*models.Market, error) {
	onchain, err := GetMarketOnchain(marketID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch onchain market: %w", err)
	}

	market, err := GetMarketByID(ctx, marketID)
	if err != nil {
		return nil, fmt.Errorf("market not found in Firestore: %w", err)
	}

	updates := []firestore.Update{}

	if onchain.IsResolved && market.Status != models.MarketStatusResolved {
		updates = append(updates, firestore.Update{Path: "status", Value: string(models.MarketStatusResolved)})

		if onchain.Outcome != nil {
			outcome := models.OutcomeYes
			if *onchain.Outcome == 1 {
				outcome = models.OutcomeNo
			}
			updates = append(updates, firestore.Update{Path: "outcome", Value: string(outcome)})
		}

		if onchain.EndPrice != nil {
			updates = append(updates, firestore.Update{Path: "end_price", Value: *onchain.EndPrice})
		}

		updates = append(updates, firestore.Update{Path: "outcome_description", Value: onchain.OutcomeDescription})

		now := time.Now()
		updates = append(updates, firestore.Update{Path: "resolved_at", Value: now})
	}

	if onchain.CurrentPrice != nil {
		updates = append(updates, firestore.Update{Path: "current_price", Value: *onchain.CurrentPrice})
	}

	if len(updates) > 0 {
		if err := updateMarket(ctx, marketID, updates); err != nil {
			return nil, fmt.Errorf("failed to sync market updates to Firestore: %w", err)
		}
	}

	return GetMarketByID(ctx, marketID)
}

func GetMarketByID(ctx context.Context, marketID string) (*models.Market, error) {
	doc, err := db.Client.Collection(marketsCollection).Doc(marketID).Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get market %s: %w", marketID, err)
	}
	var market models.Market
	if err := doc.DataTo(&market); err != nil {
		return nil, fmt.Errorf("failed to deserialize market %s: %w", marketID, err)
	}
	return &market, nil
}

func GetActiveMarkets(ctx context.Context) ([]models.Market, error) {
	return queryMarkets(ctx, db.Client.Collection(marketsCollection).
		Where("status", "==", string(models.MarketStatusActive)).
		OrderBy("created_at", firestore.Desc))
}

func GetResolvedMarkets(ctx context.Context) ([]models.Market, error) {
	return queryMarkets(ctx, db.Client.Collection(marketsCollection).
		Where("status", "==", string(models.MarketStatusResolved)).
		OrderBy("resolved_at", firestore.Desc))
}

func GetMarketsByType(ctx context.Context, marketType models.MarketType) ([]models.Market, error) {
	return queryMarkets(ctx, db.Client.Collection(marketsCollection).
		Where("market_type", "==", string(marketType)).
		OrderBy("created_at", firestore.Desc))
}

func GetMarketsByAsset(ctx context.Context, asset string) ([]models.Market, error) {
	return queryMarkets(ctx, db.Client.Collection(marketsCollection).
		Where("asset", "==", asset).
		OrderBy("created_at", firestore.Desc))
}

func GetActiveMarketsByType(ctx context.Context, marketType models.MarketType) ([]models.Market, error) {
	return queryMarkets(ctx, db.Client.Collection(marketsCollection).
		Where("status", "==", string(models.MarketStatusActive)).
		Where("market_type", "==", string(marketType)).
		OrderBy("created_at", firestore.Desc))
}

func spawnSettlementTimer(market *models.Market) {
	go func() {
		delay := time.Until(market.EndTimeUTC)
		if delay < 0 {
			delay = 0
		}

		log.Printf("[Markets] Settlement timer started: id=%s fires_in=%.0fs", market.ID, delay.Seconds())
		time.Sleep(delay)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		if err := autoSettleCAPPM(ctx, market.ID, market.Asset); err != nil {
			log.Printf("[Markets] Auto-settlement failed: id=%s err=%v", market.ID, err)
		}
	}()
}

func autoSettleCAPPM(ctx context.Context, marketID, asset string) error {
	market, err := GetMarketByID(ctx, marketID)
	if err != nil {
		return fmt.Errorf("failed to load market for auto-settlement: %w", err)
	}

	if market.Status == models.MarketStatusResolved {
		log.Printf("[Markets] Auto-settlement skipped, already resolved: id=%s", marketID)
		return nil
	}

	endPriceCents, err := GetHistoricalPrice(asset, market.EndTimeUTC)
	if err != nil {
		return fmt.Errorf("failed to fetch end price for %s at %s: %w", asset, market.EndTimeUTC, err)
	}

	return SettleCAPPM(ctx, marketID, endPriceCents)
}

func saveMarket(ctx context.Context, market *models.Market) error {
	_, err := db.Client.Collection(marketsCollection).Doc(market.ID).Set(ctx, market)
	return err
}

func updateMarket(ctx context.Context, marketID string, updates []firestore.Update) error {
	_, err := db.Client.Collection(marketsCollection).Doc(marketID).Update(ctx, updates)
	return err
}

func queryMarkets(ctx context.Context, q firestore.Query) ([]models.Market, error) {
	iter := q.Documents(ctx)
	var markets []models.Market
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error iterating markets: %w", err)
		}
		var market models.Market
		if err := doc.DataTo(&market); err != nil {
			log.Printf("[Markets] Failed to deserialize market doc %s: %v", doc.Ref.ID, err)
			continue
		}
		markets = append(markets, market)
	}
	return markets, nil
}

func resolveCAPPMOutcome(direction models.MarketDirection, targetPrice, endPrice uint64) models.MarketOutcome {
	switch direction {
	case models.DirectionAbove:
		if endPrice >= targetPrice {
			return models.OutcomeYes
		}
		return models.OutcomeNo
	case models.DirectionBelow:
		if endPrice < targetPrice {
			return models.OutcomeYes
		}
		return models.OutcomeNo
	default:
		return models.OutcomeNo
	}
}