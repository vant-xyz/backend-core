package markets

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"time"

	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/models"
)

const PRODUCTION = false

const (
	cappmMinYesProbability = 0.35
	cappmMaxYesZScore      = 0.385320466
)

var cappmLog = log.New(os.Stdout, "[CAPPM-SERVICE] ", log.Ldate|log.Ltime|log.Lmicroseconds)

type assetDurationConfig struct {
	Asset        string
	DataProvider string
	Durations    []durationConfig
}

type durationConfig struct {
	Seconds                  uint64
	Label                    string
	FallbackVolatilityFactor float64
}

var devAssetConfigs = []assetDurationConfig{
	{
		Asset:        "BTC",
		DataProvider: "coinbase",
		Durations: []durationConfig{
			{Seconds: 180, Label: "3min", FallbackVolatilityFactor: 0.004},
			{Seconds: 300, Label: "5min", FallbackVolatilityFactor: 0.006},
		},
	},
}

var prodAssetConfigs = []assetDurationConfig{
	{
		Asset:        "BTC",
		DataProvider: "coinbase",
		Durations: []durationConfig{
			{Seconds: 180, Label: "3min", FallbackVolatilityFactor: 0.004},
			{Seconds: 300, Label: "5min", FallbackVolatilityFactor: 0.006},
			{Seconds: 900, Label: "15min", FallbackVolatilityFactor: 0.010},
			{Seconds: 21600, Label: "6hr", FallbackVolatilityFactor: 0.035},
		},
	},
	{
		Asset:        "ETH",
		DataProvider: "coinbase",
		Durations: []durationConfig{
			{Seconds: 3600, Label: "1hr", FallbackVolatilityFactor: 0.020},
			{Seconds: 21600, Label: "6hr", FallbackVolatilityFactor: 0.035},
		},
	},
	{
		Asset:        "SOL",
		DataProvider: "coinbase",
		Durations: []durationConfig{
			{Seconds: 1800, Label: "30min", FallbackVolatilityFactor: 0.015},
			{Seconds: 3600, Label: "1hr", FallbackVolatilityFactor: 0.020},
			{Seconds: 21600, Label: "6hr", FallbackVolatilityFactor: 0.035},
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
	enableAutoCurated := os.Getenv("ENABLE_AUTO_CURATED_CAPPMS") == "true"
	
	configs := activeAssetConfigs()
	total := 0
	for _, a := range configs {
		total += len(a.Durations)
	}
	
	if enableAutoCurated {
		cappmLog.Printf("Starting AUTO-CREATION mode — PRODUCTION=%v, loops=%d", PRODUCTION, total)
		for _, assetCfg := range configs {
			for _, dur := range assetCfg.Durations {
				go runCappmLoop(assetCfg, dur)
			}
		}
	} else {
		cappmLog.Printf("Starting SETTLEMENT-ONLY mode — PRODUCTION=%v, will settle existing markets but not create new ones", PRODUCTION)
		for _, assetCfg := range configs {
			for _, dur := range assetCfg.Durations {
				go runCappmSettlementLoop(assetCfg, dur)
			}
		}
	}
}

func runCappmLoop(asset assetDurationConfig, dur durationConfig) {
	loopID := fmt.Sprintf("%s-%s", asset.Asset, dur.Label)
	cappmLog.Printf("[%s] Loop started", loopID)

	for {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		existing, err := db.FindActiveCappmMarket(ctx, asset.Asset, dur.Seconds)
		cancel()

		if err != nil {
			cappmLog.Printf("[%s] Error querying active market: %v — retrying in 30s", loopID, err)
			time.Sleep(30 * time.Second)
			continue
		}

		if existing != nil {
			if time.Now().Before(existing.EndTimeUTC) {
				cappmLog.Printf("[%s] Active market found: id=%s sleeping until %s",
					loopID, existing.ID, existing.EndTimeUTC.Format(time.RFC3339))
				StartLiquidityProvider(existing)
				sleepUntilTime(existing.EndTimeUTC)
				cappmLog.Printf("[%s] Market expired, creating next", loopID)
			} else {
				cappmLog.Printf("[%s] Stale expired market found: id=%s — settling",
					loopID, existing.ID)
				settleWithRetry(loopID, existing.ID, asset.Asset)
			}
			continue
		}

		cappmLog.Printf("[%s] No active market, creating now", loopID)

		market, err := createNextCappm(asset, dur)
		if err != nil {
			cappmLog.Printf("[%s] Failed to create market: %v — retrying in 30s", loopID, err)
			time.Sleep(30 * time.Second)
			continue
		}

		cappmLog.Printf("[%s] Created: id=%s target=%d direction=%s expires=%s",
			loopID, market.ID, market.TargetPrice, market.Direction,
			market.EndTimeUTC.Format(time.RFC3339))

		StartLiquidityProvider(market)

		sleepUntilTime(market.EndTimeUTC)
		cappmLog.Printf("[%s] Market expired, creating next", loopID)
	}
}

func settleWithRetry(loopID, marketID, asset string) {
	attempt := 0
	for {
		attempt++

		if attempt > 1 {
			raw := attempt * attempt * 10
			if raw > 600 {
				raw = 600
			}
			backoff := time.Duration(raw) * time.Second
			cappmLog.Printf("[%s] Settlement retry %d for %s — waiting %s",
				loopID, attempt, marketID, backoff)
			time.Sleep(backoff)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)

		market, err := db.GetMarketByID(ctx, marketID)
		if err != nil {
			cancel()
			cappmLog.Printf("[%s] Failed to load market %s (attempt %d): %v",
				loopID, marketID, attempt, err)
			continue
		}

		if market.Status == models.MarketStatusResolved {
			cancel()
			cappmLog.Printf("[%s] Market %s already resolved externally", loopID, marketID)
			return
		}

		endPriceCents, err := GetHistoricalPrice(asset, market.EndTimeUTC)
		if err != nil {
			cancel()
			cappmLog.Printf("[%s] Price fetch failed for %s (attempt %d): %v",
				loopID, marketID, attempt, err)
			continue
		}

		err = SettleCAPPM(ctx, marketID, endPriceCents)
		cancel()

		if err != nil {
			cappmLog.Printf("[%s] Settlement tx failed for %s (attempt %d): %v",
				loopID, marketID, attempt, err)
			continue
		}

		cappmLog.Printf("[%s] Settlement succeeded for %s on attempt %d", loopID, marketID, attempt)
		return
	}
}

func createNextCappm(asset assetDurationConfig, dur durationConfig) (*models.Market, error) {
	currentPriceCents, err := GetCurrentPrice(asset.Asset)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch current price for %s: %w", asset.Asset, err)
	}

	momentumPriceCents, err := GetMomentumPrice(asset.Asset)
	if err != nil {
		cappmLog.Printf("[%s-%s] Momentum price unavailable, defaulting direction to Above: %v",
			asset.Asset, dur.Label, err)
		momentumPriceCents = currentPriceCents
	}

	volatilityFactor := GetATRVolatilityFactor(asset.Asset, dur.Seconds, dur.FallbackVolatilityFactor)

	cappmLog.Printf("[%s-%s] volatility_factor=%.6f (fallback=%.6f) current=%d momentum=%d",
		asset.Asset, dur.Label, volatilityFactor, dur.FallbackVolatilityFactor,
		currentPriceCents, momentumPriceCents)

	direction, targetPriceCents := calculateTarget(currentPriceCents, momentumPriceCents, dur.Seconds, volatilityFactor)
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

func calculateTarget(currentCents, momentumCents, durationSeconds uint64, volatilityFactor float64) (models.MarketDirection, uint64) {
	direction := selectCAPPMDirection(currentCents, momentumCents, durationSeconds)
	distance := calculateCAPPMStrikeDistance(currentCents, durationSeconds, volatilityFactor)

	if direction == models.DirectionAbove {
		return direction, currentCents + distance
	}
	if currentCents > distance {
		return direction, currentCents - distance
	}
	return direction, 1
}

func selectCAPPMDirection(currentCents, momentumCents, durationSeconds uint64) models.MarketDirection {
	if currentCents > 0 {
		deltaBps := (float64(momentumCents) - float64(currentCents)) / float64(currentCents) * 10000
		threshold := directionMomentumThresholdBps(durationSeconds)
		if deltaBps >= threshold {
			return models.DirectionBelow
		}
		if deltaBps <= -threshold {
			return models.DirectionAbove
		}
	}

	seed := (currentCents / 100) + (durationSeconds / 60)
	if seed%2 == 0 {
		return models.DirectionAbove
	}
	return models.DirectionBelow
}

func calculateCAPPMStrikeDistance(currentCents, durationSeconds uint64, volatilityFactor float64) uint64 {
	if currentCents == 0 {
		return 1
	}

	durationMultiplier := strikeDurationMultiplier(durationSeconds)
	sigma := float64(currentCents) * volatilityFactor * durationMultiplier
	if sigma < 1 {
		sigma = 1
	}

	minDistance := float64(currentCents) * minDistanceBps(durationSeconds) / 10000.0
	if minDistance < 1 {
		minDistance = 1
	}

	maxDistanceByDuration := float64(currentCents) * maxDistanceBps(durationSeconds) / 10000.0
	maxDistanceByBand := sigma * cappmMaxYesZScore

	maxDistance := math.Min(maxDistanceByDuration, maxDistanceByBand)
	if maxDistance < 1 {
		maxDistance = 1
	}
	if maxDistance < minDistance {
		minDistance = maxDistance
	}

	distance := sigma * targetSigmaMultiplier(durationSeconds)
	if distance < minDistance {
		distance = minDistance
	}
	if distance > maxDistance {
		distance = maxDistance
	}

	if estimateCAPPMYesProbability(distance, sigma) < cappmMinYesProbability {
		distance = math.Min(distance, sigma*cappmMaxYesZScore)
	}

	if distance < 1 {
		distance = 1
	}
	return uint64(math.Round(distance))
}

func estimateCAPPMYesProbability(distance, sigma float64) float64 {
	if sigma <= 0 {
		return 0.5
	}

	z := distance / sigma
	return 1 - 0.5*(1+math.Erf(z/math.Sqrt2))
}

func targetSigmaMultiplier(durationSeconds uint64) float64 {
	switch {
	case durationSeconds <= 300:
		return 0.26
	case durationSeconds <= 900:
		return 0.28
	case durationSeconds <= 3600:
		return 0.30
	default:
		return 0.32
	}
}

func strikeDurationMultiplier(durationSeconds uint64) float64 {
	switch {
	case durationSeconds <= 300:
		return 0.45
	case durationSeconds <= 900:
		return 0.60
	case durationSeconds <= 3600:
		return 0.80
	default:
		return 1.00
	}
}

func minDistanceBps(durationSeconds uint64) float64 {
	switch {
	case durationSeconds <= 300:
		return 8
	case durationSeconds <= 900:
		return 12
	case durationSeconds <= 3600:
		return 18
	default:
		return 25
	}
}

func maxDistanceBps(durationSeconds uint64) float64 {
	switch {
	case durationSeconds <= 300:
		return 45
	case durationSeconds <= 900:
		return 65
	case durationSeconds <= 3600:
		return 100
	default:
		return 160
	}
}

func directionMomentumThresholdBps(durationSeconds uint64) float64 {
	switch {
	case durationSeconds <= 300:
		return 18
	case durationSeconds <= 900:
		return 14
	case durationSeconds <= 3600:
		return 10
	default:
		return 8
	}
}

func durationLabelForSeconds(durationSeconds uint64) string {
	switch durationSeconds {
	case 180:
		return "3min"
	case 300:
		return "5min"
	case 900:
		return "15min"
	case 1800:
		return "30min"
	case 3600:
		return "1hr"
	case 21600:
		return "6hr"
	default:
		return fmt.Sprintf("%ds", durationSeconds)
	}
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

func sleepUntilTime(t time.Time) {
	if delay := time.Until(t); delay > 0 {
		time.Sleep(delay)
	}
}

func runCappmSettlementLoop(asset assetDurationConfig, dur durationConfig) {
	loopID := fmt.Sprintf("%s-%s-SETTLE", asset.Asset, dur.Label)
	cappmLog.Printf("[%s] Settlement-only loop started", loopID)

	for {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		existing, err := db.FindActiveCappmMarket(ctx, asset.Asset, dur.Seconds)
		cancel()

		if err != nil {
			cappmLog.Printf("[%s] Error querying active market: %v — retrying in 5m", loopID, err)
			time.Sleep(5 * time.Minute)
			continue
		}

		if existing != nil {
			if time.Now().Before(existing.EndTimeUTC) {
				cappmLog.Printf("[%s] Active market found: id=%s, monitoring until %s",
					loopID, existing.ID, existing.EndTimeUTC.Format(time.RFC3339))
				StartLiquidityProvider(existing)
				sleepUntilTime(existing.EndTimeUTC)
				cappmLog.Printf("[%s] Market expired, settling", loopID)
			} else {
				cappmLog.Printf("[%s] Market already expired: id=%s, settling now",
					loopID, existing.ID)
			}
			
			settleWithRetry(loopID, existing.ID, asset.Asset)
			cappmLog.Printf("[%s] Settlement complete, sleeping 5m", loopID)
			time.Sleep(5 * time.Minute)
			continue
		}

		cappmLog.Printf("[%s] No active market found, sleeping 5m", loopID)
		time.Sleep(5 * time.Minute)
	}
}
