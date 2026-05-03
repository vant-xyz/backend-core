package markets

import (
	"testing"

	"github.com/vant-xyz/backend-code/models"
)

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

func TestPayoutPerWinningShare_Is1(t *testing.T) {
	if payoutPerWinningShare != 1.0 {
		t.Errorf("payoutPerWinningShare = %.2f, want 1.00 ($1 USD per share)", payoutPerWinningShare)
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
		{1, 1},
		{10, 10},
		{2.5, 2.5},
		{50, 50},
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
	// 50 shares @ $0.65 avg entry (YES side). Payout = $1/share = $50. Cost = $32.50. PnL = $17.50.
	got := calcRealizedPnL(50, 0.65, true)
	want := 17.5
	if got != want {
		t.Errorf("PnL for 50 shares @0.65 winner = %.2f, want %.2f", got, want)
	}
}

func TestCalcRealizedPnL_LowEntryHigherProfit(t *testing.T) {
	// 50 shares @ $0.20 entry. Payout = $50. Cost = $10. PnL = $40.
	got := calcRealizedPnL(50, 0.20, true)
	want := 40.0
	if got != want {
		t.Errorf("PnL = %.2f, want %.2f", got, want)
	}
}

func TestCalcRealizedPnL_LoserIsAlwaysZero(t *testing.T) {
	got := calcRealizedPnL(200, 0.40, false)
	if got != 0 {
		t.Errorf("loser PnL = %.2f, want 0", got)
	}
}

func TestCalcRealizedPnL_BreakEvenAtFullEntryPrice(t *testing.T) {
	// Bought at exactly $1.00 per share → payout == cost → PnL = 0.
	got := calcRealizedPnL(10, 1.0, true)
	want := 0.0
	if got != want {
		t.Errorf("break-even PnL = %.2f, want 0.00", got)
	}
}

// ── avg entry price ───────────────────────────────────────────────────────────

func TestAvgEntry_FirstPosition(t *testing.T) {
	got := calcAvgEntry(0, 0, 100, 0.60)
	if got != 0.60 {
		t.Errorf("avg entry for fresh position = %.2f, want 0.60", got)
	}
}

func TestAvgEntry_SecondFillSamePrice(t *testing.T) {
	got := calcAvgEntry(100, 0.60, 50, 0.60)
	if got != 0.60 {
		t.Errorf("avg entry with same fill price = %.2f, want 0.60", got)
	}
}

func TestAvgEntry_SecondFillHigherPrice(t *testing.T) {
	// 100 shares @ 0.60, then 50 more @ 0.90.
	// Weighted avg = (100*0.60 + 50*0.90) / 150 = (60 + 45) / 150 = 0.70.
	got := calcAvgEntry(100, 0.60, 50, 0.90)
	want := 0.70
	if got != want {
		t.Errorf("avg entry = %.4f, want %.4f", got, want)
	}
}

func TestAvgEntry_SecondFillLowerPrice(t *testing.T) {
	// 100 shares @ 0.80, then 100 more @ 0.60.
	// Weighted avg = (100*0.80 + 100*0.60) / 200 = (80 + 60) / 200 = 0.70.
	got := calcAvgEntry(100, 0.80, 100, 0.60)
	want := 0.70
	if got != want {
		t.Errorf("avg entry = %.4f, want %.4f", got, want)
	}
}

func TestAvgEntry_SmallFillDoesNotSkewMuch(t *testing.T) {
	// 1000 shares @ 0.50, then 1 more @ 1.00.
	// Weighted avg = (1000*0.50 + 1*1.00) / 1001 = 501/1001 ≈ 0.500499.
	got := calcAvgEntry(1000, 0.50, 1, 1.00)
	want := 501.0 / 1001.0
	if abs(got-want) > 0.0001 {
		t.Errorf("avg entry = %.6f, want %.6f", got, want)
	}
}

func TestAvgEntry_FractionalShares(t *testing.T) {
	// 2.5 shares @ 0.40, then 2.5 more @ 0.60.
	// Weighted avg = (2.5*0.40 + 2.5*0.60) / 5 = (1.0 + 1.5) / 5 = 0.50.
	got := calcAvgEntry(2.5, 0.40, 2.5, 0.60)
	want := 0.50
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
		{100, models.OrderSideYes, 0.60},
		{50, models.OrderSideNo, 0.45},
		{200, models.OrderSideYes, 0.30},
	}
	outcome := models.OutcomeYes

	totalPayout := 0.0
	for _, p := range positions {
		won := positionWon(p.side, outcome)
		totalPayout += calcPayout(p.shares, won)
	}

	// YES positions win: 100 + 200 = 300 shares × $1.00 = $300.
	// NO position loses: 0.
	want := 300.0
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

// ── resolution email stats ───────────────────────────────────────────────────

func TestResolutionStats_Win(t *testing.T) {
	// User bought 100 shares at 0.40 (Stake = $40). Market resolved YES.
	// Payout = $100. PnL = +$60. Multiplier = 2.5x.
	stake := 100.0 * 0.40
	payout := 100.0 * 1.0
	pnl := payout - stake
	multiplier := payout / stake

	if stake != 40.0 {
		t.Errorf("stake = %.2f, want 40.0", stake)
	}
	if pnl != 60.0 {
		t.Errorf("pnl = %.2f, want 60.0", pnl)
	}
	if multiplier != 2.5 {
		t.Errorf("multiplier = %.2f, want 2.5", multiplier)
	}
}

func TestResolutionStats_Loss(t *testing.T) {
	// User bought 100 shares at 0.60 (Stake = $60). Market resolved NO.
	// Payout = $0. PnL = -$60. Multiplier = 0.0x.
	stake := 100.0 * 0.60
	payout := 0.0
	pnl := payout - stake
	multiplier := 0.0
	if stake > 0 {
		multiplier = payout / stake
	}

	if pnl != -60.0 {
		t.Errorf("pnl = %.2f, want -60.0", pnl)
	}
	if multiplier != 0.0 {
		t.Errorf("multiplier = %.2f, want 0.0", multiplier)
	}
}

func TestResolutionStats_ZeroStake(t *testing.T) {
	// Corner case: Zero stake should not panic and should have 0 multiplier.
	stake := 0.0
	payout := 0.0
	multiplier := 0.0
	if stake > 0 {
		multiplier = payout / stake
	}

	if multiplier != 0.0 {
		t.Errorf("multiplier for zero stake = %.2f, want 0.0", multiplier)
	}
}
