package markets

import (
	"math"
	"testing"

	"github.com/vant-xyz/backend-code/models"
)

// ── candleGranularityForMarket ────────────────────────────────────────────────

func TestCandleGranularityForMarket(t *testing.T) {
	cases := []struct {
		durationSecs uint64
		wantGran     string
		wantInterval int64
	}{
		{60, "ONE_MINUTE", 60},
		{180, "ONE_MINUTE", 60},
		{300, "ONE_MINUTE", 60},
		{301, "FIVE_MINUTE", 300},
		{600, "FIVE_MINUTE", 300},
		{900, "FIVE_MINUTE", 300},
		{901, "FIFTEEN_MINUTE", 900},
		{1800, "FIFTEEN_MINUTE", 900},
		{86400, "FIFTEEN_MINUTE", 900},
	}
	for _, tc := range cases {
		gran, interval := candleGranularityForMarket(tc.durationSecs)
		if gran != tc.wantGran {
			t.Errorf("duration=%ds: granularity = %q, want %q", tc.durationSecs, gran, tc.wantGran)
		}
		if interval != tc.wantInterval {
			t.Errorf("duration=%ds: interval = %d, want %d", tc.durationSecs, interval, tc.wantInterval)
		}
	}
}

// ── trendBucketInterval ───────────────────────────────────────────────────────

func TestTrendBucketInterval(t *testing.T) {
	cases := []struct {
		durationSecs uint64
		wantBucket   int64
	}{
		{60, 30},
		{300, 30},
		{301, 60},
		{900, 60},
		{901, 300},
		{3600, 300},
		{3601, 900},
		{86400, 900},
	}
	for _, tc := range cases {
		got := trendBucketInterval(tc.durationSecs)
		if got != tc.wantBucket {
			t.Errorf("duration=%ds: bucket = %d, want %d", tc.durationSecs, got, tc.wantBucket)
		}
	}
}

// ── computeFillPreview ────────────────────────────────────────────────────────

func TestComputeFillPreview_EmptyBook(t *testing.T) {
	result := computeFillPreview(models.OrderSideYes, 10.0, nil)
	if result.EstimatedShares != 0 {
		t.Errorf("empty book: shares = %.4f, want 0", result.EstimatedShares)
	}
	if result.FillsCompletely {
		t.Error("empty book: fills_completely should be false")
	}
	if result.TotalCost != 0 {
		t.Errorf("empty book: total_cost = %.4f, want 0", result.TotalCost)
	}
}

func TestComputeFillPreview_SingleLevelFullFill(t *testing.T) {
	asks := []OrderbookLevel{{Price: 0.50, Quantity: 30, Orders: 1}}
	// stake=10, price=0.50 → 20 shares, total_cost=10, fills completely
	result := computeFillPreview(models.OrderSideYes, 10.0, asks)

	if !result.FillsCompletely {
		t.Error("expected fills_completely = true")
	}
	if !approxEq(result.EstimatedShares, 20.0, 1e-9) {
		t.Errorf("shares = %.4f, want 20.0", result.EstimatedShares)
	}
	if !approxEq(result.TotalCost, 10.0, 1e-9) {
		t.Errorf("total_cost = %.4f, want 10.0", result.TotalCost)
	}
	if !approxEq(result.AvgPrice, 0.50, 1e-9) {
		t.Errorf("avg_price = %.4f, want 0.50", result.AvgPrice)
	}
}

func TestComputeFillPreview_SingleLevelPartialFill(t *testing.T) {
	// Only 5 shares available at 0.50 → fills 2.50, leaves 7.50 unfilled
	asks := []OrderbookLevel{{Price: 0.50, Quantity: 5, Orders: 1}}
	result := computeFillPreview(models.OrderSideYes, 10.0, asks)

	if result.FillsCompletely {
		t.Error("expected fills_completely = false")
	}
	if !approxEq(result.EstimatedShares, 5.0, 1e-9) {
		t.Errorf("shares = %.4f, want 5.0", result.EstimatedShares)
	}
	if !approxEq(result.TotalCost, 2.50, 1e-9) {
		t.Errorf("total_cost = %.4f, want 2.50", result.TotalCost)
	}
}

func TestComputeFillPreview_MultiLevelFill(t *testing.T) {
	asks := []OrderbookLevel{
		{Price: 0.40, Quantity: 10, Orders: 1}, // costs 4.00 → 10 shares
		{Price: 0.60, Quantity: 10, Orders: 1}, // costs 6.00 → 10 shares
		{Price: 0.80, Quantity: 10, Orders: 1}, // would cost 8.00 but not needed
	}
	// stake=10: first level takes 4.00 (10 shares), second takes 6.00 (10 shares) → done
	result := computeFillPreview(models.OrderSideYes, 10.0, asks)

	if !result.FillsCompletely {
		t.Error("expected fills_completely = true")
	}
	if !approxEq(result.EstimatedShares, 20.0, 1e-9) {
		t.Errorf("shares = %.4f, want 20.0", result.EstimatedShares)
	}
	if !approxEq(result.TotalCost, 10.0, 1e-9) {
		t.Errorf("total_cost = %.4f, want 10.0", result.TotalCost)
	}
	wantAvg := 10.0 / 20.0 // 0.50
	if !approxEq(result.AvgPrice, wantAvg, 1e-9) {
		t.Errorf("avg_price = %.4f, want %.4f", result.AvgPrice, wantAvg)
	}
}

func TestComputeFillPreview_ZeroStake(t *testing.T) {
	asks := []OrderbookLevel{{Price: 0.50, Quantity: 100, Orders: 1}}
	result := computeFillPreview(models.OrderSideYes, 0, asks)
	if result.FillsCompletely {
		t.Error("zero stake: fills_completely should be false")
	}
	if result.EstimatedShares != 0 {
		t.Errorf("zero stake: shares = %.4f, want 0", result.EstimatedShares)
	}
}

func TestComputeFillPreview_SideIsPreserved(t *testing.T) {
	asks := []OrderbookLevel{{Price: 0.60, Quantity: 20, Orders: 1}}
	result := computeFillPreview(models.OrderSideNo, 6.0, asks)
	if result.Side != models.OrderSideNo {
		t.Errorf("side = %s, want NO", result.Side)
	}
}

func approxEq(a, b, epsilon float64) bool {
	return math.Abs(a-b) < epsilon
}
