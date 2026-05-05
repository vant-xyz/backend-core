package markets

import (
	"context"
	"fmt"
	"sort"

	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/models"
)

type CandlePoint struct {
	Time   int64   `json:"time"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

type TrendPoint struct {
	Time      int64   `json:"time"`
	YesVolume float64 `json:"yes_volume"`
	NoVolume  float64 `json:"no_volume"`
}

type FillPreview struct {
	Side            models.OrderSide `json:"side"`
	Stake           float64          `json:"stake"`
	AvgPrice        float64          `json:"avg_price"`
	EstimatedShares float64          `json:"estimated_shares"`
	FillsCompletely bool             `json:"fills_completely"`
	TotalCost       float64          `json:"total_cost"`
}

type MarketVolumeStats struct {
	MarketID        string  `json:"market_id"`
	TradeCount      int     `json:"trade_count"`
	Volume          float64 `json:"volume"`
	YesVolume       float64 `json:"yes_volume"`
	NoVolume        float64 `json:"no_volume"`
	YesVolumeShares float64 `json:"yes_volume_shares"`
	NoVolumeShares  float64 `json:"no_volume_shares"`
}

func GetMarketCandles(ctx context.Context, marketID string) ([]CandlePoint, int64, error) {
	market, err := GetMarketByID(ctx, marketID)
	if err != nil {
		return nil, 0, err
	}
	if market.Asset == "" {
		return nil, 0, fmt.Errorf("market has no asset")
	}

	granularity, intervalSecs := candleGranularityForMarket(market.DurationSeconds)
	raw, err := fetchCandles(market.Asset, granularity, 200)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to fetch candles: %w", err)
	}

	candles := make([]CandlePoint, len(raw))
	for i, c := range raw {
		candles[i] = CandlePoint{
			Time:   c.Start,
			Open:   c.Open,
			High:   c.High,
			Low:    c.Low,
			Close:  c.Close,
			Volume: c.Volume,
		}
	}
	return candles, intervalSecs, nil
}

func candleGranularityForMarket(durationSeconds uint64) (string, int64) {
	switch {
	case durationSeconds <= 300:
		return "ONE_MINUTE", 60
	case durationSeconds <= 900:
		return "FIVE_MINUTE", 300
	default:
		return "FIFTEEN_MINUTE", 900
	}
}

func GetOpinionTrend(ctx context.Context, marketID string) ([]TrendPoint, error) {
	market, err := GetMarketByID(ctx, marketID)
	if err != nil {
		return nil, err
	}

	orders, err := db.GetMarketFilledOrders(ctx, marketID)
	if err != nil {
		return nil, err
	}

	bucketSecs := trendBucketInterval(market.DurationSeconds)
	start := market.StartTimeUTC.Unix()

	type bucket struct{ yes, no float64 }
	buckets := make(map[int64]*bucket)

	for _, o := range orders {
		t := o.UpdatedAt.Unix()
		key := start + ((t-start)/bucketSecs)*bucketSecs
		b, ok := buckets[key]
		if !ok {
			b = &bucket{}
			buckets[key] = b
		}
		if o.Side == models.OrderSideYes {
			b.yes += o.FilledQty
		} else {
			b.no += o.FilledQty
		}
	}

	keys := make([]int64, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	points := make([]TrendPoint, len(keys))
	for i, k := range keys {
		b := buckets[k]
		points[i] = TrendPoint{Time: k, YesVolume: b.yes, NoVolume: b.no}
	}
	return points, nil
}

func trendBucketInterval(durationSeconds uint64) int64 {
	switch {
	case durationSeconds <= 300:
		return 30
	case durationSeconds <= 900:
		return 60
	case durationSeconds <= 3600:
		return 300
	default:
		return 900
	}
}

func GetFillPreview(_ context.Context, marketID string, side models.OrderSide, stake float64) (*FillPreview, error) {
	asks := GetMatchingEngine().GetDepth(marketID, side, "asks")
	result := computeFillPreview(side, stake, asks)
	return &result, nil
}

func computeFillPreview(side models.OrderSide, stake float64, asks []OrderbookLevel) FillPreview {
	remaining := stake
	totalShares := 0.0
	totalCost := 0.0

	for _, level := range asks {
		if remaining <= 0 {
			break
		}
		levelCost := level.Price * level.Quantity
		if remaining >= levelCost {
			totalShares += level.Quantity
			totalCost += levelCost
			remaining -= levelCost
		} else {
			totalShares += remaining / level.Price
			totalCost += remaining
			remaining = 0
		}
	}

	avgPrice := 0.0
	if totalShares > 0 {
		avgPrice = totalCost / totalShares
	}

	return FillPreview{
		Side:            side,
		Stake:           stake,
		AvgPrice:        avgPrice,
		EstimatedShares: totalShares,
		FillsCompletely: stake > 0 && remaining == 0,
		TotalCost:       totalCost,
	}
}

func GetMarketTrades(ctx context.Context, marketID string, limit int) ([]models.Order, error) {
	return db.GetMarketTrades(ctx, marketID, limit)
}

func GetMarketVolumeStats(ctx context.Context, marketID string) (*MarketVolumeStats, error) {
	filled, err := db.GetMarketFilledOrders(ctx, marketID)
	if err != nil {
		return nil, err
	}

	stats := &MarketVolumeStats{
		MarketID: marketID,
	}

	for _, o := range filled {
		if o.FilledQty <= 0 {
			continue
		}
		stats.TradeCount++
		notional := o.Price * o.FilledQty
		stats.Volume += notional
		if o.Side == models.OrderSideYes {
			stats.YesVolume += notional
			stats.YesVolumeShares += o.FilledQty
		} else {
			stats.NoVolume += notional
			stats.NoVolumeShares += o.FilledQty
		}
	}

	return stats, nil
}
