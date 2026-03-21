package markets

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/models"
	"google.golang.org/api/iterator"
)

const PRODUCTION = false

var cappmLog = log.New(os.Stdout, "[CAPPM-SERVICE] ", log.Ldate|log.Ltime|log.Lmicroseconds)

type assetDurationConfig struct {
	Asset        string
	DataProvider string
	Durations    []durationConfig
}

type durationConfig struct {
	Seconds          uint64
	Label            string
	VolatilityFactor float64
}

var devAssetConfigs = []assetDurationConfig{
	{
		Asset:        "BTC",
		DataProvider: "Coinbase",
		Durations: []durationConfig{
			{Seconds: 180, Label: "3min", VolatilityFactor: 0.004},
			{Seconds: 300, Label: "5min", VolatilityFactor: 0.006},
		},
	},
}

var prodAssetConfigs = []assetDurationConfig{
	{
		Asset:        "BTC",
		DataProvider: "Coinbase",
		Durations: []durationConfig{
			{Seconds: 180, Label: "3min", VolatilityFactor: 0.004},
			{Seconds: 300, Label: "5min", VolatilityFactor: 0.006},
			{Seconds: 900, Label: "15min", VolatilityFactor: 0.010},
			{Seconds: 21600, Label: "6hr", VolatilityFactor: 0.035},
		},
	},
	{
		Asset:        "ETH",
		DataProvider: "Coinbase",
		Durations: []durationConfig{
			{Seconds: 3600, Label: "1hr", VolatilityFactor: 0.020},
			{Seconds: 21600, Label: "6hr", VolatilityFactor: 0.035},
		},
	},
	{
		Asset:        "SOL",
		DataProvider: "Coinbase",
		Durations: []durationConfig{
			{Seconds: 1800, Label: "30min", VolatilityFactor: 0.015},
			{Seconds: 3600, Label: "1hr", VolatilityFactor: 0.020},
			{Seconds: 21600, Label: "6hr", VolatilityFactor: 0.035},
		},
	},
}

func activeAssetConfigs() []assetDurationConfig {
	if PRODUCTION {
		return prodAssetConfigs
	}
	return devAssetConfigs
}

func StartCAPPMService() {
	configs := activeAssetConfigs()

	total := 0
	for _, a := range configs {
		total += len(a.Durations)
	}

	cappmLog.Printf("Starting CAPPM service — PRODUCTION=%v, loops=%d", PRODUCTION, total)

	for _, assetCfg := range configs {
		for _, dur := range assetCfg.Durations {
			go runCappmLoop(assetCfg, dur)
		}
	}
}

func runCappmLoop(asset assetDurationConfig, dur durationConfig) {
	loopID := fmt.Sprintf("%s-%s", asset.Asset, dur.Label)
	cappmLog.Printf("[%s] Loop started", loopID)

	for {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		existing, err := findActiveCappmMarket(ctx, asset.Asset, dur.Seconds)
		cancel()

		if err != nil {
			cappmLog.Printf("[%s] Error querying active market: %v — retrying in 30s", loopID, err)
			time.Sleep(30 * time.Second)
			continue
		}

		if existing != nil {
			cappmLog.Printf("[%s] Active market found: id=%s sleeping until %s",
				loopID, existing.ID, existing.EndTimeUTC.Format(time.RFC3339))
			sleepUntilTime(existing.EndTimeUTC)
			cappmLog.Printf("[%s] Market expired, creating next", loopID)
			continue
		}

		cappmLog.Printf("[%s] No active market found, creating now", loopID)

		market, err := createNextCappm(asset, dur)
		if err != nil {
			cappmLog.Printf("[%s] Failed to create market: %v — retrying in 30s", loopID, err)
			time.Sleep(30 * time.Second)
			continue
		}

		cappmLog.Printf("[%s] Created: id=%s target=%d direction=%s expires=%s",
			loopID, market.ID, market.TargetPrice, market.Direction,
			market.EndTimeUTC.Format(time.RFC3339))

		sleepUntilTime(market.EndTimeUTC)
		cappmLog.Printf("[%s] Market expired, creating next", loopID)
	}
}

func createNextCappm(asset assetDurationConfig, dur durationConfig) (*models.Market, error) {
	currentPriceCents, err := GetCurrentPrice(asset.Asset)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch current price for %s: %w", asset.Asset, err)
	}

	momentumPriceCents, err := GetMomentumPrice(asset.Asset)
	if err != nil {
		cappmLog.Printf("Warning: could not fetch momentum price for %s, defaulting direction to Above: %v", asset.Asset, err)
		momentumPriceCents = currentPriceCents
	}

	direction, targetPriceCents := calculateTarget(currentPriceCents, momentumPriceCents, dur.VolatilityFactor)

	startTime := time.Now().UTC().Add(5 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	return CreateCAPPM(ctx, CreateCAPPMInput{
		Title:           buildMarketTitle(asset.Asset, direction, targetPriceCents, dur.Label),
		Description:     buildMarketDescription(asset.Asset, direction, targetPriceCents, dur.Label),
		Asset:           asset.Asset,
		Direction:       direction,
		TargetPrice:     targetPriceCents,
		CurrentPrice:    currentPriceCents,
		DataProvider:    asset.DataProvider,
		StartTimeUTC:    startTime,
		DurationSeconds: dur.Seconds,
	})
}

func calculateTarget(currentCents, momentumCents uint64, volatilityFactor float64) (models.MarketDirection, uint64) {
	direction := models.DirectionAbove
	if momentumCents < currentCents {
		direction = models.DirectionBelow
	}

	offset := uint64(float64(currentCents) * volatilityFactor)
	if offset == 0 {
		offset = 1
	}

	var targetCents uint64
	if direction == models.DirectionAbove {
		targetCents = currentCents + offset
	} else {
		if currentCents > offset {
			targetCents = currentCents - offset
		} else {
			targetCents = 1
		}
	}

	return direction, targetCents
}

func buildMarketTitle(asset string, direction models.MarketDirection, targetCents uint64, durationLabel string) string {
	dollars := targetCents / 100
	cents := targetCents % 100
	dirWord := "above"
	if direction == models.DirectionBelow {
		dirWord = "below"
	}
	return fmt.Sprintf("Will %s be %s $%d.%02d in %s?", asset, dirWord, dollars, cents, durationLabel)
}

func buildMarketDescription(asset string, direction models.MarketDirection, targetCents uint64, durationLabel string) string {
	dollars := targetCents / 100
	cents := targetCents % 100
	dirWord := "above or equal to"
	if direction == models.DirectionBelow {
		dirWord = "strictly below"
	}
	return fmt.Sprintf(
		"Resolves YES if %s spot price is %s $%d.%02d at expiry (%s from creation). Price sourced from Coinbase.",
		asset, dirWord, dollars, cents, durationLabel,
	)
}

func findActiveCappmMarket(ctx context.Context, asset string, durationSeconds uint64) (*models.Market, error) {
	iter := db.Client.Collection(marketsCollection).
		Where("market_type", "==", string(models.MarketTypeCAPPM)).
		Where("status", "==", string(models.MarketStatusActive)).
		Where("asset", "==", asset).
		Where("duration_seconds", "==", durationSeconds).
		Limit(1).
		Documents(ctx)

	doc, err := iter.Next()
	if err == iterator.Done {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("firestore query failed: %w", err)
	}

	var market models.Market
	if err := doc.DataTo(&market); err != nil {
		return nil, fmt.Errorf("failed to deserialize market: %w", err)
	}
	return &market, nil
}

func sleepUntilTime(t time.Time) {
	if delay := time.Until(t); delay > 0 {
		time.Sleep(delay)
	}
}