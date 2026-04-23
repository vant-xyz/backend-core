package markets

import (
	"strings"
	"testing"

	"github.com/vant-xyz/backend-code/models"
)

// ── calculateTarget ───────────────────────────────────────────────────────────

func TestCalculateTarget_MomentumHigher_DirectionAbove(t *testing.T) {
	dir, _ := calculateTarget(10000, 11000, 0.01)
	if dir != models.DirectionAbove {
		t.Errorf("expected DirectionAbove when momentum > current, got %s", dir)
	}
}

func TestCalculateTarget_MomentumLower_DirectionBelow(t *testing.T) {
	dir, _ := calculateTarget(10000, 9000, 0.01)
	if dir != models.DirectionBelow {
		t.Errorf("expected DirectionBelow when momentum < current, got %s", dir)
	}
}

func TestCalculateTarget_EqualMomentum_DefaultsToAbove(t *testing.T) {
	dir, _ := calculateTarget(10000, 10000, 0.01)
	if dir != models.DirectionAbove {
		t.Errorf("expected DirectionAbove when momentum == current, got %s", dir)
	}
}

func TestCalculateTarget_AboveOffset_IsVolatilityPercent(t *testing.T) {
	// current = $1000.00 (100000 cents), vol = 1% → offset = 1000 → target = 101000
	_, target := calculateTarget(100000, 200000, 0.01)
	want := uint64(101000)
	if target != want {
		t.Errorf("above target = %d, want %d", target, want)
	}
}

func TestCalculateTarget_BelowOffset_IsVolatilityPercent(t *testing.T) {
	// current = $1000.00 (100000 cents), vol = 1% → offset = 1000 → target = 99000
	_, target := calculateTarget(100000, 50000, 0.01)
	want := uint64(99000)
	if target != want {
		t.Errorf("below target = %d, want %d", target, want)
	}
}

func TestCalculateTarget_ZeroOffset_ClampsToOne(t *testing.T) {
	// current = 1 cent, vol = 0.01% → offset rounds to 0 → clamped to 1 → target = 2
	_, target := calculateTarget(1, 100, 0.0001)
	if target != 2 {
		t.Errorf("zero-offset clamp: target = %d, want 2 (current + minimum offset 1)", target)
	}
}

func TestCalculateTarget_BelowOffsetExceedsPrice_ClampsToOne(t *testing.T) {
	// current = 5 cents, vol = 100% → offset = 5, 5 > 5 is false → target = 1
	_, target := calculateTarget(5, 1, 1.0)
	if target != 1 {
		t.Errorf("below underflow should clamp to 1, got %d", target)
	}
}

// ── buildMarketTitle ──────────────────────────────────────────────────────────

func TestBuildMarketTitle_Above(t *testing.T) {
	// $150.00 = 15000 cents
	got := buildMarketTitle("SOL", models.DirectionAbove, 15000, "3min")
	want := "Will SOL be above $150.00 in 3min?"
	if got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
}

func TestBuildMarketTitle_Below(t *testing.T) {
	// $2500.99 = 250099 cents
	got := buildMarketTitle("ETH", models.DirectionBelow, 250099, "1hr")
	want := "Will ETH be below $2500.99 in 1hr?"
	if got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
}

func TestBuildMarketTitle_CentsFormatsTwoDigits(t *testing.T) {
	// $100500.00 = 10050000 cents
	got := buildMarketTitle("BTC", models.DirectionAbove, 10050000, "5min")
	want := "Will BTC be above $100500.00 in 5min?"
	if got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
}

// ── buildMarketDescription ────────────────────────────────────────────────────

func TestBuildMarketDescription_Above_ContainsAboveOrEqualTo(t *testing.T) {
	desc := buildMarketDescription("BTC", models.DirectionAbove, 10000000, "5min")
	if !strings.Contains(desc, "above or equal to") {
		t.Errorf("above description should contain 'above or equal to', got: %s", desc)
	}
	if !strings.Contains(desc, "Coinbase") {
		t.Errorf("description should reference Coinbase, got: %s", desc)
	}
}

func TestBuildMarketDescription_Below_ContainsStrictlyBelow(t *testing.T) {
	desc := buildMarketDescription("ETH", models.DirectionBelow, 250000, "1hr")
	if !strings.Contains(desc, "strictly below") {
		t.Errorf("below description should contain 'strictly below', got: %s", desc)
	}
}

func TestBuildMarketDescription_ContainsAssetAndDuration(t *testing.T) {
	desc := buildMarketDescription("SOL", models.DirectionAbove, 15000, "30min")
	if !strings.Contains(desc, "SOL") {
		t.Errorf("description missing asset name, got: %s", desc)
	}
	if !strings.Contains(desc, "30min") {
		t.Errorf("description missing duration, got: %s", desc)
	}
}
