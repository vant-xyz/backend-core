package markets

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	coinbaseAPIBase      = "https://api.coinbase.com/v2"
	coinbaseAdvancedHost = "api.coinbase.com"
	coinbaseAdvancedBase = "https://api.coinbase.com/api/v3/brokerage"

	atrCandleCount       = 14
	momentumLookbackSecs = 300
	priceCacheTTL        = 10 * time.Second
	historicalCacheTTL   = 5 * time.Minute
	maxRetries           = 3
	retryBackoffBase     = 500 * time.Millisecond
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

// buildCoinbaseJWT builds a CDP Ed25519 JWT matching the Coinbase Python SDK behaviour:
//
//	header:  {"alg":"EdDSA","kid":"<key_id>"}
//	payload: {"sub":"<key_id>","iss":"cdp","nbf":<now>,"exp":<now+120>,"uri":"GET api.coinbase.com<path_without_query>"}
//	key:     first 32 bytes of the base64-decoded secret are the Ed25519 seed
func buildCoinbaseJWT(method, pathWithQuery string) (string, error) {
	keyID := os.Getenv("COINBASE_API_KEY")
	keySecret := os.Getenv("COINBASE_API_SECRET")

	if keyID == "" || keySecret == "" {
		return "", fmt.Errorf("COINBASE_API_KEY or COINBASE_API_SECRET not set")
	}

	raw, err := base64.StdEncoding.DecodeString(keySecret)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(keySecret)
		if err != nil {
			return "", fmt.Errorf("failed to decode coinbase secret: %w", err)
		}
	}

	if len(raw) < 32 {
		return "", fmt.Errorf("coinbase secret too short: %d bytes (need at least 32)", len(raw))
	}

	privKey := ed25519.NewKeyFromSeed(raw[:32])

	parsedURL, err := url.Parse("https://" + coinbaseAdvancedHost + pathWithQuery)
	if err != nil {
		return "", fmt.Errorf("failed to parse path: %w", err)
	}
	pathOnly := parsedURL.Path

	now := time.Now().Unix()
	uri := fmt.Sprintf("%s %s%s", strings.ToUpper(method), coinbaseAdvancedHost, pathOnly)

	headerJSON, _ := json.Marshal(map[string]string{
		"alg": "EdDSA",
		"kid": keyID,
	})
	payloadJSON, _ := json.Marshal(map[string]interface{}{
		"sub": keyID,
		"iss": "cdp",
		"nbf": now,
		"exp": now + 120,
		"uri": uri,
	})

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)
	signingInput := headerB64 + "." + payloadB64

	sig := ed25519.Sign(privKey, []byte(signingInput))

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

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

func GetHistoricalPrice(asset string, at time.Time) (uint64, error) {
	cacheKey := fmt.Sprintf("%s_%d", asset, at.Unix())

	historicalCacheMu.RLock()
	if cached, ok := historicalCache[cacheKey]; ok && time.Since(cached.fetchedAt) < historicalCacheTTL {
		historicalCacheMu.RUnlock()
		return cached.valueCents, nil
	}
	historicalCacheMu.RUnlock()

	cents, err := fetchMinutePriceCents(asset, at)
	if err != nil {
		return 0, err
	}

	historicalCacheMu.Lock()
	historicalCache[cacheKey] = cachedPrice{valueCents: cents, fetchedAt: time.Now()}
	historicalCacheMu.Unlock()

	return cents, nil
}

func fetchMinutePriceCents(asset string, at time.Time) (uint64, error) {
	start := at.Add(-3 * time.Minute)
	end := at.Add(3 * time.Minute)

	pathWithQuery := fmt.Sprintf(
		"/api/v3/brokerage/products/%s-USD/candles?start=%d&end=%d&granularity=ONE_MINUTE",
		asset, start.Unix(), end.Unix(),
	)
	fullURL := "https://" + coinbaseAdvancedHost + pathWithQuery

	var result struct {
		Candles []struct {
			Start string `json:"start"`
			Close string `json:"close"`
		} `json:"candles"`
	}

	if err := doRequest("GET", fullURL, pathWithQuery, true, &result); err != nil {
		return 0, fmt.Errorf("historical candle fetch failed for %s at %s: %w", asset, at.Format(time.RFC3339), err)
	}

	if len(result.Candles) == 0 {
		return 0, fmt.Errorf("no candle data for %s at %s", asset, at.Format(time.RFC3339))
	}

	targetUnix := at.Unix()
	bestClose := ""
	var bestDiff int64 = math.MaxInt64

	for _, c := range result.Candles {
		ts, _ := strconv.ParseInt(c.Start, 10, 64)
		if targetUnix >= ts && targetUnix < ts+60 {
			bestClose = c.Close
			break
		}
		diff := targetUnix - ts
		if diff < 0 {
			diff = -diff
		}
		if diff < bestDiff {
			bestDiff = diff
			bestClose = c.Close
		}
	}

	if bestClose == "" {
		return 0, fmt.Errorf("could not find candle close for %s at %s", asset, at.Format(time.RFC3339))
	}

	dollars, err := strconv.ParseFloat(bestClose, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse candle close %q: %w", bestClose, err)
	}
	return uint64(math.Round(dollars * 100)), nil
}

func GetMomentumPrice(asset string) (uint64, error) {
	lookback := time.Now().UTC().Add(-momentumLookbackSecs * time.Second)
	return fetchMinutePriceCents(asset, lookback)
}

func GetATRVolatilityFactor(asset string, durationSeconds uint64, fallback float64) float64 {
	granularity, candleCount := atrParams(durationSeconds)

	candles, err := fetchCandles(asset, granularity, candleCount+1)
	if err != nil || len(candles) < 2 {
		cappmLog.Printf("ATR fallback for %s (dur=%ds): %v", asset, durationSeconds, err)
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

	factor := atr / (float64(currentCents) / 100.0)
	return math.Max(fallback*0.25, math.Min(fallback*4.0, factor))
}

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

func computeATR(candles []coinbaseCandle) float64 {
	if len(candles) < 2 {
		return 0
	}
	var trSum float64
	for i := 1; i < len(candles); i++ {
		curr, prev := candles[i], candles[i-1]
		tr := math.Max(curr.High-curr.Low,
			math.Max(math.Abs(curr.High-prev.Close), math.Abs(curr.Low-prev.Close)))
		trSum += tr
	}
	return trSum / float64(len(candles)-1)
}

func fetchSpotPriceCents(asset string, at time.Time) (uint64, error) {
	url := fmt.Sprintf("%s/prices/%s-USD/spot", coinbaseAPIBase, asset)
	if !at.IsZero() {
		url += fmt.Sprintf("?date=%s", at.UTC().Format("2006-01-02"))
	}

	var result struct {
		Data struct {
			Amount string `json:"amount"`
		} `json:"data"`
	}

	if err := doRequest("GET", url, "", false, &result); err != nil {
		return 0, fmt.Errorf("failed to fetch spot price for %s: %w", asset, err)
	}

	dollars, err := strconv.ParseFloat(result.Data.Amount, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse price %q: %w", result.Data.Amount, err)
	}
	return uint64(math.Round(dollars * 100)), nil
}

func fetchCandles(asset, granularity string, count int) ([]coinbaseCandle, error) {
	cacheKey := fmt.Sprintf("%s_%s_%d", asset, granularity, count)

	candleCacheMu.RLock()
	if cached, ok := candleCache[cacheKey]; ok && time.Since(cached.fetchedAt) < priceCacheTTL {
		candleCacheMu.RUnlock()
		return cached.candles, nil
	}
	candleCacheMu.RUnlock()

	end := time.Now().UTC()
	start := candleStartTime(end, granularity, count)

	pathWithQuery := fmt.Sprintf(
		"/api/v3/brokerage/products/%s-USD/candles?start=%d&end=%d&granularity=%s",
		asset, start.Unix(), end.Unix(), granularity,
	)
	fullURL := "https://" + coinbaseAdvancedHost + pathWithQuery

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

	if err := doRequest("GET", fullURL, pathWithQuery, true, &result); err != nil {
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
		candles = append(candles, coinbaseCandle{startTs, low, high, open, close_, volume})
	}

	for i, j := 0, len(candles)-1; i < j; i, j = i+1, j-1 {
		candles[i], candles[j] = candles[j], candles[i]
	}

	candleCacheMu.Lock()
	candleCache[cacheKey] = cachedCandles{candles: candles, fetchedAt: time.Now()}
	candleCacheMu.Unlock()

	return candles, nil
}

func candleStartTime(end time.Time, granularity string, count int) time.Time {
	intervals := map[string]int64{
		"ONE_MINUTE": 60, "FIVE_MINUTE": 300, "FIFTEEN_MINUTE": 900,
		"ONE_HOUR": 3600, "SIX_HOUR": 21600,
	}
	secs, ok := intervals[granularity]
	if !ok {
		secs = 300
	}
	return end.Add(-time.Duration(int64(count)*secs) * time.Second)
}

func getWithRetry(url string, dest interface{}) error {
	return doRequest("GET", url, "", false, dest)
}

func doRequest(method, fullURL, pathWithQuery string, authed bool, dest interface{}) error {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(retryBackoffBase * time.Duration(1<<uint(attempt-1)))
		}

		req, err := http.NewRequest(method, fullURL, nil)
		if err != nil {
			lastErr = fmt.Errorf("failed to create request (attempt %d): %w", attempt+1, err)
			continue
		}

		if authed {
			jwt, err := buildCoinbaseJWT(method, pathWithQuery)
			if err != nil {
				return fmt.Errorf("failed to build coinbase JWT: %w", err)
			}
			req.Header.Set("Authorization", "Bearer "+jwt)
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := httpClient.Do(req)
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