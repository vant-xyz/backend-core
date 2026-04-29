package markets

import (
	"strings"
	"testing"

	"github.com/vant-xyz/backend-code/models"
)

// ── direction selection ──────────────────────────────────────────────────────

func TestSelectCAPPMDirection_StrongMomentumDrivesDirection(t *testing.T) {
	dir := selectCAPPMDirection(10000, 12000, 300)
	if dir != models.DirectionAbove {
		t.Fatalf("expected DirectionAbove on strong positive momentum, got %s", dir)
	}

	dir = selectCAPPMDirection(10000, 8000, 300)
	if dir != models.DirectionBelow {
		t.Fatalf("expected DirectionBelow on strong negative momentum, got %s", dir)
	}
}

func TestSelectCAPPMDirection_WeakMomentumFallsBackToBalancedSeed(t *testing.T) {
	dir := selectCAPPMDirection(10000, 10010, 300)
	if dir != models.DirectionBelow {
		t.Fatalf("expected fallback direction Below for even/odd seed logic, got %s", dir)
	}
}

// ── strike distance ──────────────────────────────────────────────────────────

func TestCalculateCAPPMStrikeDistance_IsDurationAware(t *testing.T) {
	short := calculateCAPPMStrikeDistance(100000, 300, 0.02)
	long := calculateCAPPMStrikeDistance(100000, 21600, 0.02)

	if long <= short {
		t.Fatalf("expected longer duration distance to exceed short duration: short=%d long=%d", short, long)
	}
}

func TestCalculateCAPPMStrikeDistance_ClampsToTradableBand(t *testing.T) {
	current := uint64(100000)
	distance := calculateCAPPMStrikeDistance(current, 300, 0.75)

	maxBand := uint64(float64(current) * 0.75 * strikeDurationMultiplier(300) * cappmMaxYesZScore)
	if distance > maxBand {
		t.Fatalf("distance=%d exceeds tradable band max=%d", distance, maxBand)
	}
	if distance == 0 {
		t.Fatal("distance should never be zero")
	}
}

// ── calculateTarget ──────────────────────────────────────────────────────────

func TestCalculateTarget_UsesDurationAwareDistance(t *testing.T) {
	_, shortTarget := calculateTarget(100000, 100000, 300, 0.02)
	_, longTarget := calculateTarget(100000, 100000, 21600, 0.02)

	shortDelta := absDiffU64(shortTarget, 100000)
	longDelta := absDiffU64(longTarget, 100000)

	if longDelta <= shortDelta {
		t.Fatalf("expected longer duration target to sit farther from spot: short=%d long=%d", shortDelta, longDelta)
	}
}

func TestCalculateTarget_AboveAndBelowRemainSymmetricAroundSpot(t *testing.T) {
	_, above := calculateTarget(100000, 120000, 300, 0.02)
	_, below := calculateTarget(100000, 80000, 300, 0.02)

	if above <= 100000 {
		t.Fatalf("expected above target to sit above spot, got %d", above)
	}
	if below >= 100000 {
		t.Fatalf("expected below target to sit below spot, got %d", below)
	}
}

// ── duration labels ──────────────────────────────────────────────────────────

func TestDurationLabelForSeconds_KnownDurations(t *testing.T) {
	cases := map[uint64]string{
		180:   "3min",
		300:   "5min",
		900:   "15min",
		1800:  "30min",
		3600:  "1hr",
		21600: "6hr",
		123:   "123s",
	}

	for seconds, want := range cases {
		if got := durationLabelForSeconds(seconds); got != want {
			t.Fatalf("duration label for %d = %q, want %q", seconds, got, want)
		}
	}
}

// ── buildMarketTitle ──────────────────────────────────────────────────────────

func TestBuildMarketTitle_Above(t *testing.T) {
	got := buildMarketTitle("SOL", models.DirectionAbove, 15000, "3min")
	want := "Will SOL be above $150.00 in 3min?"
	if got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
}

func TestBuildMarketTitle_Below(t *testing.T) {
	got := buildMarketTitle("ETH", models.DirectionBelow, 250099, "1hr")
	want := "Will ETH be below $2500.99 in 1hr?"
	if got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
}

func TestBuildMarketTitle_CentsFormatsTwoDigits(t *testing.T) {
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

func absDiffU64(a, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return b - a
}
