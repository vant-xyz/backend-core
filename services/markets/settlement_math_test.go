package markets

import (
	"testing"

	"github.com/vant-xyz/backend-code/models"
)

// Payout and PnL formulas extracted from settlement.go and position.go
// for deterministic testing without DB.

func calcPayout(shares float64, won bool) float64 {
	if won {
		return shares * payoutPerWinningShare
	}
	return 0
}

func calcRealizedPnL(shares, avgEntryPrice float64, won bool) float64 {
	payout := calcPayout(shares, won)
	if payout == 0 {
		return 0
	}
	return payout - (shares * avgEntryPrice)
}

func calcAvgEntry(existingShares, existingAvg, newShares, fillPrice float64) float64 {
	totalShares := existingShares + newShares
	return ((existingShares * existingAvg) + (newShares * fillPrice)) / totalShares
}

func positionWon(side models.OrderSide, outcome models.MarketOutcome) bool {
	return (side == models.OrderSideYes && outcome == models.OutcomeYes) ||
		(side == models.OrderSideNo && outcome == models.OutcomeNo)
}

// ── payout per share constant ─────────────────────────────────────────────────

func TestPayoutPerWinningShare_Is100(t *testing.T) {
	if payoutPerWinningShare != 100.0 {
		t.Errorf("payoutPerWinningShare = %.2f, want 100.00", payoutPerWinningShare)
	}
}

// ── win condition ─────────────────────────────────────────────────────────────

func TestWinCondition(t *testing.T) {
	cases := []struct {
		side    models.OrderSide
		outcome models.MarketOutcome
		want    bool
	}{
		{models.OrderSideYes, models.OutcomeYes, true},
		{models.OrderSideNo, models.OutcomeNo, true},
		{models.OrderSideYes, models.OutcomeNo, false},
		{models.OrderSideNo, models.OutcomeYes, false},
	}
	for _, tc := range cases {
		got := positionWon(tc.side, tc.outcome)
		if got != tc.want {
			t.Errorf("positionWon(side=%s, outcome=%s) = %v, want %v",
				tc.side, tc.outcome, got, tc.want)
		}
	}
}

// ── payout calculation ────────────────────────────────────────────────────────

func TestCalcPayout_WinnerGetsSharesTimesPayout(t *testing.T) {
	cases := []struct {
		shares float64
		want   float64
	}{
		{1, 100},
		{10, 1000},
		{2.5, 250},
		{0.5, 50},
	}
	for _, tc := range cases {
		got := calcPayout(tc.shares, true)
		if got != tc.want {
			t.Errorf("calcPayout(%.1f, true) = %.2f, want %.2f", tc.shares, got, tc.want)
		}
	}
}

func TestCalcPayout_LoserGetsZero(t *testing.T) {
	got := calcPayout(500, false)
	if got != 0 {
		t.Errorf("calcPayout(500, false) = %.2f, want 0.00 (loser payout)", got)
	}
}

// ── realized PnL ──────────────────────────────────────────────────────────────

func TestCalcRealizedPnL_WinnerProfit(t *testing.T) {
	// Bought 100 shares @ $60 avg entry. Payout = $100/share.
	// Cost = 100 * 60 = 6000. Payout = 100 * 100 = 10000. PnL = 4000.
	got := calcRealizedPnL(100, 60, true)
	want := 4000.0
	if got != want {
		t.Errorf("PnL for 100 shares @60 winner = %.2f, want %.2f", got, want)
	}
}

func TestCalcRealizedPnL_LowEntryHigherProfit(t *testing.T) {
	// 50 shares @ $20 entry. Payout = 5000. Cost = 1000. PnL = 4000.
	got := calcRealizedPnL(50, 20, true)
	want := 4000.0
	if got != want {
		t.Errorf("PnL = %.2f, want %.2f", got, want)
	}
}

func TestCalcRealizedPnL_LoserIsAlwaysZero(t *testing.T) {
	got := calcRealizedPnL(200, 40, false)
	if got != 0 {
		t.Errorf("loser PnL = %.2f, want 0", got)
	}
}

func TestCalcRealizedPnL_BreakEvenAt100EntryPrice(t *testing.T) {
	// Bought at exactly $100 per share → payout == cost → PnL = 0.
	got := calcRealizedPnL(10, 100, true)
	want := 0.0
	if got != want {
		t.Errorf("break-even PnL = %.2f, want 0.00", got)
	}
}

// ── avg entry price ───────────────────────────────────────────────────────────

func TestAvgEntry_FirstPosition(t *testing.T) {
	// First fill: avg entry IS the fill price.
	got := calcAvgEntry(0, 0, 100, 60)
	if got != 60 {
		t.Errorf("avg entry for fresh position = %.2f, want 60", got)
	}
}

func TestAvgEntry_SecondFillSamePrice(t *testing.T) {
	// Same price both fills → avg stays same.
	got := calcAvgEntry(100, 60, 50, 60)
	if got != 60 {
		t.Errorf("avg entry with same fill price = %.2f, want 60", got)
	}
}

func TestAvgEntry_SecondFillHigherPrice(t *testing.T) {
	// 100 shares @ 60, then 50 more @ 90.
	// Weighted avg = (100*60 + 50*90) / 150 = (6000 + 4500) / 150 = 70.
	got := calcAvgEntry(100, 60, 50, 90)
	want := 70.0
	if got != want {
		t.Errorf("avg entry = %.4f, want %.4f", got, want)
	}
}

func TestAvgEntry_SecondFillLowerPrice(t *testing.T) {
	// 100 shares @ 80, then 100 more @ 60.
	// Weighted avg = (100*80 + 100*60) / 200 = (8000 + 6000) / 200 = 70.
	got := calcAvgEntry(100, 80, 100, 60)
	want := 70.0
	if got != want {
		t.Errorf("avg entry = %.4f, want %.4f", got, want)
	}
}

func TestAvgEntry_SmallFillDoesNotSkewMuch(t *testing.T) {
	// 1000 shares @ 50, then 1 more @ 100.
	// Weighted avg = (1000*50 + 1*100) / 1001 = 50100/1001 ≈ 50.05.
	got := calcAvgEntry(1000, 50, 1, 100)
	want := 50100.0 / 1001.0
	if abs(got-want) > 0.0001 {
		t.Errorf("avg entry = %.6f, want %.6f", got, want)
	}
}

func TestAvgEntry_FractionalShares(t *testing.T) {
	// 2.5 shares @ 40, then 2.5 more @ 60.
	// Weighted avg = (2.5*40 + 2.5*60) / 5 = (100 + 150) / 5 = 50.
	got := calcAvgEntry(2.5, 40, 2.5, 60)
	want := 50.0
	if got != want {
		t.Errorf("avg entry fractional = %.4f, want %.4f", got, want)
	}
}

// ── total payout across multiple positions ────────────────────────────────────

func TestTotalPayout_MultipleMixedPositions(t *testing.T) {
	type pos struct {
		shares  float64
		side    models.OrderSide
		avgCost float64
	}

	positions := []pos{
		{100, models.OrderSideYes, 60},
		{50, models.OrderSideNo, 45},
		{200, models.OrderSideYes, 30},
	}
	outcome := models.OutcomeYes

	totalPayout := 0.0
	for _, p := range positions {
		won := positionWon(p.side, outcome)
		totalPayout += calcPayout(p.shares, won)
	}

	// YES positions win: 100 + 200 = 300 shares * 100 = 30000.
	// NO position loses: 0.
	want := 30000.0
	if totalPayout != want {
		t.Errorf("total payout = %.2f, want %.2f", totalPayout, want)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
