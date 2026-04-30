package markets

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/vant-xyz/backend-code/models"
)

type mockMarketDependencies struct {
	PlaceOrderFunc    func(ctx context.Context, input PlaceOrderInput) (*models.Order, error)
	GetUserOrdersFunc func(ctx context.Context, email, marketID string) ([]models.Order, error)
	CancelOrderFunc   func(ctx context.Context, orderID, userEmail string) error
	GetCurrentPriceFunc func(asset string) (uint64, error)
}

func (m *mockMarketDependencies) PlaceOrder(ctx context.Context, input PlaceOrderInput) (*models.Order, error) {
	if m.PlaceOrderFunc != nil {
		return m.PlaceOrderFunc(ctx, input)
	}
	return nil, nil
}

func (m *mockMarketDependencies) GetUserOrders(ctx context.Context, email, marketID string) ([]models.Order, error) {
	if m.GetUserOrdersFunc != nil {
		return m.GetUserOrdersFunc(ctx, email, marketID)
	}
	return nil, nil
}

func (m *mockMarketDependencies) CancelOrder(ctx context.Context, orderID, userEmail string) error {
	if m.CancelOrderFunc != nil {
		return m.CancelOrderFunc(ctx, orderID, userEmail)
	}
	return nil
}

func (m *mockMarketDependencies) GetCurrentPrice(asset string) (uint64, error) {
    if m.GetCurrentPriceFunc != nil {
        return m.GetCurrentPriceFunc(asset)
    }
    return 0, nil
}

func TestLiquidityProvider_ProbabilityMath(t *testing.T) {
	tests := []struct {
		name              string
		currentCents      uint64
		targetPrice       uint64
		direction         models.MarketDirection
		durationSeconds   uint64
		expectedProbRange [2]float64
	}{
		{
			name:              "BTC Above, current far below target",
			currentCents:      9000000,
			targetPrice:       10000000,
			direction:         models.DirectionAbove,
			durationSeconds:   300,
			expectedProbRange: [2]float64{0.05, 0.20},
		},
		{
			name:              "BTC Above, current far above target",
			currentCents:      11000000,
			targetPrice:       10000000,
			direction:         models.DirectionAbove,
			durationSeconds:   300,
			expectedProbRange: [2]float64{0.80, 0.95},
		},
		{
			name:              "BTC Above, current near target",
			currentCents:      10000000,
			targetPrice:       10000000,
			direction:         models.DirectionAbove,
			durationSeconds:   300,
			expectedProbRange: [2]float64{0.40, 0.60},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			market := &models.Market{
				ID:              "test-market",
				Asset:           "BTC",
				Direction:       tt.direction,
				TargetPrice:     tt.targetPrice,
				DurationSeconds: tt.durationSeconds,
			}

			current := float64(tt.currentCents)
			target := float64(tt.targetPrice)
			volatility := GetATRVolatilityFactor(market.Asset, market.DurationSeconds, 0.005)
			
			z := (current - target) / (target * volatility)
			if market.Direction == models.DirectionBelow {
				z = -z
			}
			prob := 0.5 * (1 + math.Erf(z/math.Sqrt(2)))
			
			if prob < 0.05 { prob = 0.05 }
			if prob > 0.95 { prob = 0.95 }

			assert.True(t, prob >= tt.expectedProbRange[0] && prob <= tt.expectedProbRange[1],
				"Calculated probability %f is outside expected range %v", prob, tt.expectedProbRange)
		})
	}
}

func TestFairValueProb_AboveCurrentAboveTarget(t *testing.T) {
	prob := fairValueProb(110000, 100000, models.DirectionAbove, 0.01, 1.0)
	assert.Greater(t, prob, 0.7, "expected high YES probability when price well above target")
}

func TestFairValueProb_BelowCurrentAboveTarget(t *testing.T) {
	prob := fairValueProb(90000, 100000, models.DirectionAbove, 0.01, 1.0)
	assert.Less(t, prob, 0.3, "expected low YES probability when price well below target")
}

func TestFairValueProb_DirectionBelowInverts(t *testing.T) {
	above := fairValueProb(110000, 100000, models.DirectionAbove, 0.01, 1.0)
	below := fairValueProb(110000, 100000, models.DirectionBelow, 0.01, 1.0)
	sum := above + below
	assert.InDelta(t, 1.0, sum, 0.001, "Above + Below for same inputs should sum to 1.0")
}

func TestFairValueProb_AtTargetNearHalf(t *testing.T) {
	prob := fairValueProb(100000, 100000, models.DirectionAbove, 0.01, 1.0)
	assert.InDelta(t, 0.5, prob, 0.05, "expected ~0.5 when current == target")
}

func TestFairValueProb_LateMarketMoreExtreme(t *testing.T) {
	early := fairValueProb(101000, 100000, models.DirectionAbove, 0.05, 1.0)
	late := fairValueProb(101000, 100000, models.DirectionAbove, 0.05, 0.1)
	assert.Greater(t, late, early, "late-market probability should be more extreme than early-market")
}

func TestFairValueProb_ClampedToRange(t *testing.T) {
	low := fairValueProb(10000, 100000, models.DirectionAbove, 0.0001, 1.0)
	high := fairValueProb(1000000, 100000, models.DirectionAbove, 0.0001, 1.0)
	assert.GreaterOrEqual(t, low, 0.03)
	assert.LessOrEqual(t, high, 0.97)
}

func TestMatchingEngine_Rehydration(t *testing.T) {
	marketID := "MKT_REHYDRATE"
	email1 := "test1@vant.xyz"
	email2 := "test2@vant.xyz"

	ordersInDB := []models.Order{
		{
			ID: "ORD_1", UserEmail: email1, MarketID: marketID, Side: models.OrderSideYes,
			Type: models.OrderTypeLimit, Price: 0.51, Quantity: 100, RemainingQty: 100,
			Status: models.OrderStatusOpen, CreatedAt: time.Now().Add(-5 * time.Minute),
		},
		{
			ID: "ORD_2", UserEmail: email2, MarketID: marketID, Side: models.OrderSideNo,
			Type: models.OrderTypeLimit, Price: 0.49, Quantity: 50, RemainingQty: 50,
			Status: models.OrderStatusPartiallyFilled, CreatedAt: time.Now().Add(-4 * time.Minute),
		},
	}

	originalGetOpenOrders := getOpenOrdersForMarketFn
	getOpenOrdersForMarketFn = func(ctx context.Context, mid string) ([]models.Order, error) {
		if mid == marketID {
			return ordersInDB, nil
		}
		return nil, nil
	}
	defer func() { getOpenOrdersForMarketFn = originalGetOpenOrders }()

	engine := &MatchingEngine{books: make(map[string]*marketBook)}
	engine.getOrCreateBook(marketID)

	time.Sleep(100 * time.Millisecond)

	yesBids := engine.GetDepth(marketID, models.OrderSideYes, "bids")
	noBids := engine.GetDepth(marketID, models.OrderSideNo, "bids")

	assert.Len(t, yesBids, 1)
	assert.Equal(t, 0.51, yesBids[0].Price)
	assert.Equal(t, 100.0, yesBids[0].Quantity)

	assert.Len(t, noBids, 1)
	assert.Equal(t, 0.49, noBids[0].Price)
	assert.Equal(t, 50.0, noBids[0].Quantity)
}
