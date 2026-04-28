package markets

import (
	"context"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/vant-xyz/backend-code/models"
)

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
		{
			name:              "BTC Below, current far below target",
			currentCents:      9000000,
			targetPrice:       10000000,
			direction:         models.DirectionBelow,
			durationSeconds:   300,
			expectedProbRange: [2]float64{0.80, 0.95},
		},
		{
			name:              "BTC Below, current far above target",
			currentCents:      11000000,
			targetPrice:       10000000,
			direction:         models.DirectionBelow,
			durationSeconds:   300,
			expectedProbRange: [2]float64{0.05, 0.20},
		},
		{
			name:              "BTC Below, current near target",
			currentCents:      10000000,
			targetPrice:       10000000,
			direction:         models.DirectionBelow,
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

			if prob < tt.expectedProbRange[0] || prob > tt.expectedProbRange[1] {
				t.Errorf("Calculated probability %f is outside expected range %v", prob, tt.expectedProbRange)
			}
		})
	}
}

func TestMatchingEngine_Rehydration(t *testing.T) {
	marketID := "MKT_REHYDRATE"
	email1 := "test1@vant.xyz"
	email2 := "test2@vant.xyz"

	ordersInDB := []*models.Order{
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
		{
			ID: "ORD_3", UserEmail: email1, MarketID: marketID, Side: models.OrderSideYes,
			Type: models.OrderTypeLimit, Price: 0.50, Quantity: 200, RemainingQty: 150,
			Status: models.OrderStatusPartiallyFilled, CreatedAt: time.Now().Add(-3 * time.Minute),
		},
	}

	db.GetOpenOrdersForMarket = func(ctx context.Context, mid string) ([]models.Order, error) {
		if mid == marketID {
			var result []models.Order
			for _, o := range ordersInDB {
				result = append(result, *o)
			}
			return result, nil
		}
		return nil, nil
	}

	engine := GetMatchingEngine()
	book := engine.getOrCreateBook(marketID)

	time.Sleep(100 * time.Millisecond)

	yesBids := book.GetDepth(marketID, models.OrderSideYes, "bids")
	noBids := book.GetDepth(marketID, models.OrderSideNo, "bids")

	if len(yesBids) != 2 {
		t.Fatalf("Expected 2 YES bids after rehydration, got %d", len(yesBids))
	}
	if yesBids[0].Price != 0.51 || yesBids[0].Quantity != 100 {
		t.Errorf("Unexpected YES bid 0: %+v", yesBids[0])
	}
	if yesBids[1].Price != 0.50 || yesBids[1].Quantity != 150 {
		t.Errorf("Unexpected YES bid 1: %+v", yesBids[1])
	}

	if len(noBids) != 1 {
		t.Fatalf("Expected 1 NO bid after rehydration, got %d", len(noBids))
	}
	if noBids[0].Price != 0.49 || noBids[0].Quantity != 50 {
		t.Errorf("Unexpected NO bid 0: %+v", noBids[0])
	}
}

func TestSeedInitialLiquidity_OrderGeneration(t *testing.T) {
	market := &models.Market{
		ID:              "test-seed-market",
		MarketType:      models.MarketTypeCAPPM,
		Asset:           "BTC",
		Direction:       models.DirectionAbove,
		TargetPrice:     10000000,
		DurationSeconds: 300,
	}
	
	ordersPlaced := make(chan PlaceOrderInput, 20)

	originalPlaceOrder := PlaceOrder
	PlaceOrder = func(ctx context.Context, input PlaceOrderInput) (*models.Order, error) {
		ordersPlaced <- input
		return &models.Order{ID: "ORD_TEST"}, nil
	}
	defer func() { PlaceOrder = originalPlaceOrder }()

	ctx := context.Background()
	seedInitialLiquidity(ctx, market)

	close(ordersPlaced)

	var yesOrders []PlaceOrderInput
	var noOrders []PlaceOrderInput

	for orderInput := range ordersPlaced {
		if orderInput.Side == models.OrderSideYes {
			yesOrders = append(yesOrders, orderInput)
		} else {
			noOrders = append(noOrders, orderInput)
		}
	}

	if len(yesOrders) != 5 || len(noOrders) != 5 {
		t.Errorf("Expected 5 YES and 5 NO orders, got %d YES and %d NO", len(yesOrders), len(noOrders))
	}

	for i := 0; i < len(yesOrders)-1; i++ {
		if yesOrders[i].Price <= yesOrders[i+1].Price {
			t.Errorf("YES orders are not sorted by price descending: %f, %f", yesOrders[i].Price, yesOrders[i+1].Price)
		}
	}
	for i := 0; i < len(noOrders)-1; i++ {
		if noOrders[i].Price <= noOrders[i+1].Price {
			t.Errorf("NO orders are not sorted by price descending: %f, %f", noOrders[i].Price, noOrders[i+1].Price)
		}
	}

	for _, order := range yesOrders {
		if order.Quantity < 50 || order.Quantity > 200 {
			t.Errorf("YES order quantity %f outside expected range", order.Quantity)
		}
		if order.Price < 0.01 || order.Price > 0.49 {
			t.Errorf("YES order price %f outside expected range (0.01-0.49)", order.Price)
		}
	}
	for _, order := range noOrders {
		if order.Quantity < 50 || order.Quantity > 200 {
			t.Errorf("NO order quantity %f outside expected range", order.Quantity)
		}
		if order.Price < 0.01 || order.Price > 0.49 {
			t.Errorf("NO order price %f outside expected range (0.01-0.49)", order.Price)
		}
	}
}

func TestUpdateLiquidity_OrderGeneration(t *testing.T) {
	market := &models.Market{
		ID:              "test-update-market",
		MarketType:      models.MarketTypeCAPPM,
		Asset:           "BTC",
		Direction:       models.DirectionAbove,
		TargetPrice:     10000000,
		DurationSeconds: 300,
	}

	currentPriceCents := uint64(10500000)

	originalGetCurrentPrice := GetCurrentPrice
	GetCurrentPrice = func(asset string) (uint64, error) {
		return currentPriceCents, nil
	}
	defer func() { GetCurrentPrice = originalGetCurrentPrice }()

	ordersPlaced := make(chan PlaceOrderInput, 5)

	originalPlaceOrder := PlaceOrder
	PlaceOrder = func(ctx context.Context, input PlaceOrderInput) (*models.Order, error) {
		ordersPlaced <- input
		return &models.Order{ID: "ORD_TEST_UPDATE"}, nil
	}
	defer func() { PlaceOrder = originalPlaceOrder }()

	originalGetUserOrders := GetUserOrders
	GetUserOrders = func(ctx context.Context, email, marketID string) ([]models.Order, error) {
		return nil, nil
	}
	defer func() { GetUserOrders = originalGetUserOrders }()

	originalCancelOrder := CancelOrder
	CancelOrder = func(ctx context.Context, orderID, userEmail string) error {
		return nil
	}
	defer func() { CancelOrder = originalCancelOrder }()

	ctx := context.Background()
	updateLiquidity(ctx, market)

	close(ordersPlaced)

	var yesOrder PlaceOrderInput
	var noOrder PlaceOrderInput
	for orderInput := range ordersPlaced {
		if orderInput.Side == models.OrderSideYes {
			yesOrder = orderInput
		} else if orderInput.Side == models.OrderSideNo {
			noOrder = orderInput
		}
	}

	if yesOrder.Quantity < 100 || yesOrder.Quantity > 500 {
		t.Errorf("YES order quantity %f outside expected range", yesOrder.Quantity)
	}
	if yesOrder.Price < 0.01 || yesOrder.Price > 0.95 {
		t.Errorf("YES order price %f outside expected range (0.01-0.95)", yesOrder.Price)
	}

	if noOrder.Quantity < 100 || noOrder.Quantity > 500 {
		t.Errorf("NO order quantity %f outside expected range", noOrder.Quantity)
	}
	if noOrder.Price < 0.01 || noOrder.Price > 0.95 {
		t.Errorf("NO order price %f outside expected range (0.01-0.95)", noOrder.Price)
	}

	expectedSum := yesOrder.Price + noOrder.Price
	if expectedSum < 0.90 || expectedSum > 1.10 {
		t.Errorf("YES + NO prices sum to %f, expected around 1.00", expectedSum)
	}
}

func TestCleanupBotOrders(t *testing.T) {
	marketID := "test-cleanup-market"
	email := botEmails[0]
	
	openOrder := models.Order{
		ID: "OPEN_ORD", UserEmail: email, MarketID: marketID, Status: models.OrderStatusOpen,
	}
	filledOrder := models.Order{
		ID: "FILLED_ORD", UserEmail: email, MarketID: marketID, Status: models.OrderStatusFilled,
	}

	getUserOrdersCalled := 0
	originalGetUserOrders := GetUserOrders
	GetUserOrders = func(ctx context.Context, e, mid string) ([]models.Order, error) {
		getUserOrdersCalled++
		if e == email && mid == marketID {
			return []models.Order{openOrder, filledOrder}, nil
		}
		return nil, nil
	}
	defer func() { GetUserOrders = originalGetUserOrders }()

	cancelOrderCalled := 0
	cancelledOrderID := ""
	originalCancelOrder := CancelOrder
	CancelOrder = func(ctx context.Context, orderID, userEmail string) error {
		cancelOrderCalled++
		cancelledOrderID = orderID
		return nil
	}
	defer func() { CancelOrder = originalCancelOrder }()

	ctx := context.Background()
	cleanupBotOrders(ctx, marketID)

	if getUserOrdersCalled != 1 {
		t.Errorf("GetUserOrders was called %d times, expected 1", getUserOrdersCalled)
	}
	if cancelOrderCalled != 1 {
		t.Errorf("CancelOrder was called %d times, expected 1", cancelOrderCalled)
	}
	if cancelledOrderID != "OPEN_ORD" {
		t.Errorf("Expected to cancel OPEN_ORD, but cancelled %s", cancelledOrderID)
	}
}
