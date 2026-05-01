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

const defaultCurrency = "USD"

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
	AssetImage      string
	Category        string
}

type CreateGEMInput struct {
	Title             string
	Description       string
	DataProvider      string
	StartTimeUTC      time.Time
	DurationSeconds   uint64
	MarketImageSmall  string
	MarketImageBanner string
	Category          string
}

type CreateCAPPMFromAdminInput struct {
	Asset           string
	DurationSeconds uint64
	StartTimeUTC    time.Time
	AssetImage      string
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
		AssetImage:      input.AssetImage,
		Category:        "crypto",
	}

	if err := db.SaveMarket(ctx, market); err != nil {
		return nil, fmt.Errorf("failed to save CAPPM market: %w", err)
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

	category := input.Category
	if category == "" {
		category = "general"
	}

	market := &models.Market{
		ID:                marketID,
		MarketType:        models.MarketTypeGEM,
		Status:            models.MarketStatusActive,
		QuoteCurrency:     defaultCurrency,
		Title:             input.Title,
		Description:       input.Description,
		DataProvider:      input.DataProvider,
		CreatorAddress:    settlerKey.PublicKey().String(),
		MarketPDA:         marketPDA.String(),
		StartTimeUTC:      input.StartTimeUTC,
		EndTimeUTC:        endTime,
		DurationSeconds:   input.DurationSeconds,
		CreatedAt:         now,
		CreationTxHash:    txHash,
		MarketImageSmall:  input.MarketImageSmall,
		MarketImageBanner: input.MarketImageBanner,
		Category:          category,
	}

	if err := db.SaveMarket(ctx, market); err != nil {
		return nil, fmt.Errorf("failed to save GEM market: %w", err)
	}

	log.Printf("[Markets] CreateGEM complete: id=%s pda=%s tx=%s", marketID, marketPDA, txHash)
	return market, nil
}

func CreateCAPPMFromAdmin(ctx context.Context, input CreateCAPPMFromAdminInput) (*models.Market, error) {
	currentCents, err := GetCurrentPrice(input.Asset)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch current price for %s: %w", input.Asset, err)
	}

	momentumCents, err := GetMomentumPrice(input.Asset)
	if err != nil {
		momentumCents = currentCents
	}

	volatilityFactor := GetATRVolatilityFactor(input.Asset, input.DurationSeconds, 0.01)
	direction, targetPrice := calculateTarget(currentCents, momentumCents, input.DurationSeconds, volatilityFactor)
	durationLabel := durationLabelForSeconds(input.DurationSeconds)
	title := buildMarketTitle(input.Asset, direction, targetPrice, durationLabel)
	description := buildMarketDescription(input.Asset, direction, targetPrice, durationLabel)

	return CreateCAPPM(ctx, CreateCAPPMInput{
		Title:           title,
		Description:     description,
		Asset:           input.Asset,
		Direction:       direction,
		TargetPrice:     targetPrice,
		CurrentPrice:    currentCents,
		DataProvider:    "coinbase",
		StartTimeUTC:    input.StartTimeUTC,
		DurationSeconds: input.DurationSeconds,
		AssetImage:      input.AssetImage,
	})
}

func SettleCAPPMOffChain(ctx context.Context, marketID string, endPriceCents uint64) error {
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

	outcome := resolveCAPPMOutcome(market.Direction, market.TargetPrice, endPriceCents)
	dollars := endPriceCents / 100
	cents := endPriceCents % 100
	outcomeDescription := fmt.Sprintf("%s closed at $%d.%02d on %s", market.Asset, dollars, cents, market.DataProvider)

	now := time.Now()
	if err := db.UpdateMarketFields(ctx, marketID, map[string]interface{}{
		"status":              string(models.MarketStatusResolved),
		"outcome":             string(outcome),
		"outcome_description": outcomeDescription,
		"end_price":           endPriceCents,
		"resolved_at":         now,
	}); err != nil {
		return fmt.Errorf("failed to update CAPPM market: %w", err)
	}

	payoutCtx, payoutCancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer payoutCancel()
	if _, err := ProcessMarketSettlement(payoutCtx, marketID, outcome); err != nil {
		log.Printf("[Markets] SettleCAPPMOffChain payout distribution failed for %s: %v", marketID, err)
	}

	log.Printf("[Markets] SettleCAPPMOffChain complete: id=%s outcome=%s endPrice=%d", marketID, outcome, endPriceCents)
	return nil
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
	outcomeDescription := fmt.Sprintf("%s closed at $%d.%02d on %s",
		market.Asset, dollars, cents, market.DataProvider)

	now := time.Now()
	if err := db.UpdateMarketFields(ctx, marketID, map[string]interface{}{
		"status":              string(models.MarketStatusResolved),
		"outcome":             string(outcome),
		"outcome_description": outcomeDescription,
		"end_price":           endPriceCents,
		"settlement_tx_hash":  txHash,
		"resolved_at":         now,
	}); err != nil {
		return fmt.Errorf("failed to update CAPPM market after settlement: %w", err)
	}

	payoutCtx, payoutCancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer payoutCancel()
	if _, err := ProcessMarketSettlement(payoutCtx, marketID, outcome); err != nil {
		log.Printf("[Markets] SettleCAPPM payout distribution failed for %s: %v", marketID, err)
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
	if err := db.UpdateMarketFields(ctx, input.MarketID, map[string]interface{}{
		"status":              string(models.MarketStatusResolved),
		"outcome":             string(input.Outcome),
		"outcome_description": input.OutcomeDescription,
		"settlement_tx_hash":  txHash,
		"resolved_at":         now,
	}); err != nil {
		return fmt.Errorf("failed to update GEM market after settlement: %w", err)
	}

	payoutCtx, payoutCancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer payoutCancel()
	if _, err := ProcessMarketSettlement(payoutCtx, input.MarketID, input.Outcome); err != nil {
		log.Printf("[Markets] SettleGEM payout distribution failed for %s: %v", input.MarketID, err)
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
		return nil, fmt.Errorf("market not found in DB: %w", err)
	}

	fields := map[string]interface{}{}

	if onchain.IsResolved && market.Status != models.MarketStatusResolved {
		fields["status"] = string(models.MarketStatusResolved)
		fields["outcome_description"] = onchain.OutcomeDescription
		fields["resolved_at"] = time.Now()

		if onchain.Outcome != nil {
			outcome := models.OutcomeYes
			if *onchain.Outcome == 1 {
				outcome = models.OutcomeNo
			}
			fields["outcome"] = string(outcome)
		}

		if onchain.EndPrice != nil {
			fields["end_price"] = *onchain.EndPrice
		}
	}

	if onchain.CurrentPrice != nil {
		fields["current_price"] = *onchain.CurrentPrice
	}

	if len(fields) > 0 {
		if err := db.UpdateMarketFields(ctx, marketID, fields); err != nil {
			return nil, fmt.Errorf("failed to sync market updates to DB: %w", err)
		}
	}

	return GetMarketByID(ctx, marketID)
}

func GetMarketByID(ctx context.Context, marketID string) (*models.Market, error) {
	return db.GetMarketByID(ctx, marketID)
}

func GetActiveMarkets(ctx context.Context) ([]models.Market, error) {
	return db.GetActiveMarkets(ctx)
}

func GetResolvedMarkets(ctx context.Context) ([]models.Market, error) {
	return db.GetResolvedMarkets(ctx)
}

func GetMarketsByType(ctx context.Context, marketType models.MarketType) ([]models.Market, error) {
	return db.GetMarketsByType(ctx, marketType)
}

func GetMarketsByAsset(ctx context.Context, asset string) ([]models.Market, error) {
	return db.GetMarketsByAsset(ctx, asset)
}

func GetActiveMarketsByType(ctx context.Context, marketType models.MarketType) ([]models.Market, error) {
	return db.GetActiveMarketsByType(ctx, marketType)
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

	if err := SettleCAPPM(ctx, marketID, endPriceCents); err != nil {
		return err
	}

	outcome := resolveCAPPMOutcome(market.Direction, market.TargetPrice, endPriceCents)
	pCtx, pCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer pCancel()
	result, pErr := ProcessMarketSettlement(pCtx, marketID, outcome)
	if pErr != nil {
		log.Printf("[Markets] Auto-settler payout distribution failed for %s: %v", marketID, pErr)
		return nil
	}
	log.Printf("[Markets] Auto-settler payouts distributed: market=%s winners=%d losers=%d payout=%.2f",
		marketID, result.WinningCount, result.LosingCount, result.TotalPayout)
	return nil
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
func GetMarketOnchainData(marketID string) (*OnchainMarket, error) {
	return GetMarketOnchain(marketID)
}
