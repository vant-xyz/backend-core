package markets

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	coinbaseAPIBase      = "https://api.coinbase.com/v2"
	coinbaseAdvancedBase = "https://api.coinbase.com/api/v3/brokerage"

	candleGranularity300   = "FIVE_MINUTE"
	candleGranularity60    = "ONE_MINUTE"
	atrCandleCount         = 14
	momentumLookbackSecs   = 300
	priceCacheTTL          = 10 * time.Second
	historicalCacheTTL     = 5 * time.Minute
	maxRetries             = 3
	retryBackoffBase       = 500 * time.Millisecond
)

var (
	priceCache     = make(map[string]cachedPrice)
	priceCacheMu   sync.RWMutex

	historicalCache   = make(map[string]cachedPrice)
	historicalCacheMu sync.RWMutex

	candleCache   = make(map[string]cachedCandles)
	candleCacheMu sync.RWMutex

	httpClient = &http.Client{Timeout: 10 * time.Second}
)

type cachedPrice struct {
	valueCents uint64
	fetchedAt  time.Time
}

type cachedCandles struct {
	candles   []coinbaseCandle
	fetchedAt time.Time
}

type coinbaseCandle struct {
	Start  int64
	Low    float64
	High   float64
	Open   float64
	Close  float64
	Volume float64
}

// GetCurrentPrice returns the current spot price for an asset in cents.
// Results are cached for priceCacheTTL to avoid hammering the API across
// multiple concurrent goroutines fetching the same asset.
func GetCurrentPrice(asset string) (uint64, error) {
	priceCacheMu.RLock()
	if cached, ok := priceCache[asset]; ok && time.Since(cached.fetchedAt) < priceCacheTTL {
		priceCacheMu.RUnlock()
		return cached.valueCents, nil
	}
	priceCacheMu.RUnlock()

	cents, err := fetchSpotPriceCents(asset, time.Time{})
	if err != nil {
		return 0, err
	}

	priceCacheMu.Lock()
	priceCache[asset] = cachedPrice{valueCents: cents, fetchedAt: time.Now()}
	priceCacheMu.Unlock()

	return cents, nil
}

// GetHistoricalPrice returns the closing price in cents for an asset at or
// just before the given timestamp. Used by the settlement service to determine
// the end price at market expiry.
func GetHistoricalPrice(asset string, at time.Time) (uint64, error) {
	cacheKey := fmt.Sprintf("%s_%d", asset, at.Unix())

	historicalCacheMu.RLock()
	if cached, ok := historicalCache[cacheKey]; ok && time.Since(cached.fetchedAt) < historicalCacheTTL {
		historicalCacheMu.RUnlock()
		return cached.valueCents, nil
	}
	historicalCacheMu.RUnlock()

	cents, err := fetchSpotPriceCents(asset, at)
	if err != nil {
		return 0, err
	}

	historicalCacheMu.Lock()
	historicalCache[cacheKey] = cachedPrice{valueCents: cents, fetchedAt: time.Now()}
	historicalCacheMu.Unlock()

	return cents, nil
}

// GetMomentumPrice returns the spot price from momentumLookbackSecs ago in
// cents. Used by the CAPPM service to determine market direction.
func GetMomentumPrice(asset string) (uint64, error) {
	lookback := time.Now().UTC().Add(-momentumLookbackSecs * time.Second)
	return fetchSpotPriceCents(asset, lookback)
}

// GetATRVolatilityFactor computes a normalised ATR-based volatility factor for
// the given asset and duration. Returns the fallback factor if candle data is
// unavailable.
func GetATRVolatilityFactor(asset string, durationSeconds uint64, fallback float64) float64 {
	granularity, candleCount := atrParams(durationSeconds)

	candles, err := fetchCandles(asset, granularity, candleCount+1)
	if err != nil || len(candles) < 2 {
		cappmLog.Printf("ATR fallback for %s (dur=%ds): could not fetch candles: %v", asset, durationSeconds, err)
		return fallback
	}

	atr := computeATR(candles)
	if atr == 0 {
		return fallback
	}

	currentCents, err := GetCurrentPrice(asset)
	if err != nil || currentCents == 0 {
		return fallback
	}

	currentDollars := float64(currentCents) / 100.0
	factor := atr / currentDollars

	// Clamp to reasonable bounds — don't let a flash crash produce a
	// 40% volatility factor on a 3-minute market.
	minFactor := fallback * 0.25
	maxFactor := fallback * 4.0
	factor = math.Max(minFactor, math.Min(maxFactor, factor))

	return factor
}

// atrParams returns the appropriate candle granularity string and number of
// candles to fetch for ATR calculation given a market duration.
func atrParams(durationSeconds uint64) (string, int) {
	switch {
	case durationSeconds <= 300:
		return "ONE_MINUTE", 14
	case durationSeconds <= 900:
		return "FIVE_MINUTE", 14
	case durationSeconds <= 3600:
		return "FIFTEEN_MINUTE", 14
	case durationSeconds <= 21600:
		return "ONE_HOUR", 14
	default:
		return "SIX_HOUR", 14
	}
}

// computeATR computes a simple 14-period Average True Range from candles.
// True Range = max(High-Low, |High-PrevClose|, |Low-PrevClose|)
func computeATR(candles []coinbaseCandle) float64 {
	if len(candles) < 2 {
		return 0
	}

	var trSum float64
	count := 0

	for i := 1; i < len(candles); i++ {
		curr := candles[i]
		prev := candles[i-1]

		tr := math.Max(
			curr.High-curr.Low,
			math.Max(
				math.Abs(curr.High-prev.Close),
				math.Abs(curr.Low-prev.Close),
			),
		)
		trSum += tr
		count++
	}

	if count == 0 {
		return 0
	}
	return trSum / float64(count)
}

// fetchSpotPriceCents fetches the spot price from Coinbase in cents.
// If at is zero, fetches the current price. Otherwise fetches the price
// at the given timestamp using the spot endpoint's date parameter.
func fetchSpotPriceCents(asset string, at time.Time) (uint64, error) {
	pair := fmt.Sprintf("%s-USD", asset)
	url := fmt.Sprintf("%s/prices/%s/spot", coinbaseAPIBase, pair)

	if !at.IsZero() {
		url += fmt.Sprintf("?date=%s", at.UTC().Format("2006-01-02"))
	}

	var result struct {
		Data struct {
			Amount string `json:"amount"`
		} `json:"data"`
	}

	if err := getWithRetry(url, &result); err != nil {
		return 0, fmt.Errorf("failed to fetch spot price for %s: %w", asset, err)
	}

	dollars, err := strconv.ParseFloat(result.Data.Amount, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse price %q for %s: %w", result.Data.Amount, asset, err)
	}

	return uint64(math.Round(dollars * 100)), nil
}

// fetchCandles fetches OHLCV candles from the Coinbase Advanced Trade API.
// Returns candles sorted oldest-first.
func fetchCandles(asset, granularity string, count int) ([]coinbaseCandle, error) {
	cacheKey := fmt.Sprintf("%s_%s_%d", asset, granularity, count)

	candleCacheMu.RLock()
	if cached, ok := candleCache[cacheKey]; ok && time.Since(cached.fetchedAt) < priceCacheTTL {
		candleCacheMu.RUnlock()
		return cached.candles, nil
	}
	candleCacheMu.RUnlock()

	productID := fmt.Sprintf("%s-USD", asset)
	end := time.Now().UTC()
	start := candleStartTime(end, granularity, count)

	url := fmt.Sprintf(
		"%s/products/%s/candles?start=%d&end=%d&granularity=%s",
		coinbaseAdvancedBase,
		productID,
		start.Unix(),
		end.Unix(),
		granularity,
	)

	var result struct {
		Candles []struct {
			Start  string `json:"start"`
			Low    string `json:"low"`
			High   string `json:"high"`
			Open   string `json:"open"`
			Close  string `json:"close"`
			Volume string `json:"volume"`
		} `json:"candles"`
	}

	if err := getWithRetry(url, &result); err != nil {
		return nil, fmt.Errorf("failed to fetch candles for %s: %w", asset, err)
	}

	candles := make([]coinbaseCandle, 0, len(result.Candles))
	for _, c := range result.Candles {
		startTs, _ := strconv.ParseInt(c.Start, 10, 64)
		low, _ := strconv.ParseFloat(c.Low, 64)
		high, _ := strconv.ParseFloat(c.High, 64)
		open, _ := strconv.ParseFloat(c.Open, 64)
		close_, _ := strconv.ParseFloat(c.Close, 64)
		volume, _ := strconv.ParseFloat(c.Volume, 64)

		candles = append(candles, coinbaseCandle{
			Start:  startTs,
			Low:    low,
			High:   high,
			Open:   open,
			Close:  close_,
			Volume: volume,
		})
	}

	// Coinbase returns candles newest-first — reverse to oldest-first for ATR
	for i, j := 0, len(candles)-1; i < j; i, j = i+1, j-1 {
		candles[i], candles[j] = candles[j], candles[i]
	}

	candleCacheMu.Lock()
	candleCache[cacheKey] = cachedCandles{candles: candles, fetchedAt: time.Now()}
	candleCacheMu.Unlock()

	return candles, nil
}

func candleStartTime(end time.Time, granularity string, count int) time.Time {
	var intervalSeconds int64
	switch granularity {
	case "ONE_MINUTE":
		intervalSeconds = 60
	case "FIVE_MINUTE":
		intervalSeconds = 300
	case "FIFTEEN_MINUTE":
		intervalSeconds = 900
	case "ONE_HOUR":
		intervalSeconds = 3600
	case "SIX_HOUR":
		intervalSeconds = 21600
	default:
		intervalSeconds = 300
	}
	return end.Add(-time.Duration(int64(count)*intervalSeconds) * time.Second)
}

// getWithRetry performs a GET request with exponential backoff on failure.
func getWithRetry(url string, dest interface{}) error {
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := retryBackoffBase * time.Duration(1<<uint(attempt-1))
			time.Sleep(backoff)
		}

		resp, err := httpClient.Get(url)
		if err != nil {
			lastErr = fmt.Errorf("request failed (attempt %d): %w", attempt+1, err)
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			lastErr = fmt.Errorf("rate limited (attempt %d)", attempt+1)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = fmt.Errorf("unexpected status %d (attempt %d)", resp.StatusCode, attempt+1)
			continue
		}

		if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
			resp.Body.Close()
			lastErr = fmt.Errorf("failed to decode response (attempt %d): %w", attempt+1, err)
			continue
		}

		resp.Body.Close()
		return nil
	}

	return fmt.Errorf("all %d attempts failed: %w", maxRetries, lastErr)
}