package markets

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/models"
	"github.com/vant-xyz/backend-code/services"
	"github.com/vant-xyz/backend-code/utils"
)

type PlaceOrderInput struct {
	UserEmail string
	MarketID  string
	Side      models.OrderSide
	Type      models.OrderType
	Price     float64
	Quantity  float64
	IsDemo    bool
	ExpiresAt *time.Time
}

type OrderbookLevel struct {
	Price    float64 `json:"price"`
	Quantity float64 `json:"quantity"`
	Orders   int     `json:"orders"`
}

type OrderbookSnapshot struct {
	MarketID        string           `json:"market_id"`
	YesBids         []OrderbookLevel `json:"yes_bids"`
	YesAsks         []OrderbookLevel `json:"yes_asks"`
	NoBids          []OrderbookLevel `json:"no_bids"`
	NoAsks          []OrderbookLevel `json:"no_asks"`
	LastTradedPrice float64          `json:"last_traded_price"`
	Spread          float64          `json:"spread"`
	FetchedAt       time.Time        `json:"fetched_at"`
}

func PlaceOrder(ctx context.Context, input PlaceOrderInput) (*models.Order, error) {
	market, err := GetMarketByID(ctx, input.MarketID)
	if err != nil {
		return nil, fmt.Errorf("market not found: %w", err)
	}
	if market.Status != models.MarketStatusActive {
		return nil, fmt.Errorf("market %s is not active", input.MarketID)
	}
	if input.Quantity <= 0 {
		return nil, fmt.Errorf("quantity must be positive")
	}
	if input.Type == models.OrderTypeLimit && input.Price <= 0 {
		return nil, fmt.Errorf("limit order requires a positive price")
	}
	if input.Type == models.OrderTypeLimit && input.Price >= 100 {
		return nil, fmt.Errorf("limit order price must be less than 100 %s per share", market.QuoteCurrency)
	}

	currency := orderBalanceCurrency(input.IsDemo)

	var lockedAmount float64
	if input.Type == models.OrderTypeLimit {
		lockedAmount = input.Price * input.Quantity
	} else {
		lockedAmount, err = estimateMarketOrderCost(input.MarketID, input.Side, input.Quantity)
		if err != nil || lockedAmount == 0 {
			globalRiskState.recordNoLiquidity(input.MarketID)
			return nil, fmt.Errorf("no liquidity available for market order on %s %s", input.MarketID, input.Side)
		}
	}

	if err := services.LockBalance(ctx, input.UserEmail, lockedAmount, currency); err != nil {
		return nil, fmt.Errorf("insufficient balance: %w", err)
	}

	now := time.Now()
	order := &models.Order{
		ID:            fmt.Sprintf("ORD_%s", utils.RandomAlphanumeric(12)),
		UserEmail:     input.UserEmail,
		MarketID:      input.MarketID,
		Side:          input.Side,
		Type:          input.Type,
		Price:         input.Price,
		Quantity:      input.Quantity,
		FilledQty:     0,
		RemainingQty:  input.Quantity,
		Status:        models.OrderStatusOpen,
		QuoteCurrency: market.QuoteCurrency,
		IsDemo:        input.IsDemo,
		CreatedAt:     now,
		UpdatedAt:     now,
		ExpiresAt:     input.ExpiresAt,
	}

	if err := db.RedisStoreOrder(ctx, order); err != nil {
		if unlockErr := services.UnlockBalance(ctx, input.UserEmail, lockedAmount, currency); unlockErr != nil {
			log.Printf("[Orders] CRITICAL: failed to unlock balance after order save failure for %s: %v",
				input.UserEmail, unlockErr)
		}
		return nil, fmt.Errorf("failed to save order: %w", err)
	}

	db.AsyncSyncOrderToPG(order, func(c context.Context, o *models.Order) error {
		return db.SaveOrder(c, o)
	})

	GetMatchingEngine().Submit(order)

	if input.ExpiresAt != nil {
		go scheduleOrderExpiry(order)
	}

	return order, nil
}

func CancelOrder(ctx context.Context, orderID, userEmail string) error {
	order, err := GetOrderByID(ctx, orderID)
	if err != nil {
		return fmt.Errorf("order not found: %w", err)
	}
	if order.UserEmail != userEmail {
		return fmt.Errorf("order %s does not belong to user %s", orderID, userEmail)
	}
	if order.Status == models.OrderStatusFilled || order.Status == models.OrderStatusCancelled {
		return fmt.Errorf("order %s cannot be cancelled (status: %s)", orderID, order.Status)
	}

	if err := db.UpdateOrderStatus(ctx, orderID, models.OrderStatusCancelled); err != nil {
		return fmt.Errorf("failed to update order status: %w", err)
	}

	GetMatchingEngine().Cancel(orderID)

	refundAmount := order.Price * order.RemainingQty
	if refundAmount > 0 {
		currency := orderBalanceCurrency(order.IsDemo)
		if err := services.UnlockBalance(ctx, userEmail, refundAmount, currency); err != nil {
			log.Printf("[Orders] CRITICAL: failed to unlock balance on cancel for order %s user %s: %v",
				orderID, userEmail, err)
		}
	}
	return nil
}

func orderBalanceCurrency(isDemo bool) string {
	if isDemo {
		return "USD_DEMO"
	}
	return "USD"
}

func GetOrderByID(ctx context.Context, orderID string) (*models.Order, error) {
	return db.GetOrderByID(ctx, orderID)
}

func GetUserOrders(ctx context.Context, userEmail, marketID string) ([]models.Order, error) {
	return db.GetUserOrders(ctx, userEmail, marketID)
}

func GetMarketOrders(ctx context.Context, marketID string, status models.OrderStatus) ([]models.Order, error) {
	return db.GetMarketOrders(ctx, marketID, status)
}

func GetOpenOrdersForMarket(ctx context.Context, marketID string) ([]models.Order, error) {
	return db.GetOpenOrdersForMarket(ctx, marketID)
}

func UpdateOrderFill(ctx context.Context, orderID string, filledQty, remainingQty float64, status models.OrderStatus) error {
	return db.UpdateOrderFill(ctx, orderID, filledQty, remainingQty, status)
}

func GetOrderbook(ctx context.Context, marketID string) (*OrderbookSnapshot, error) {
	engine := GetMatchingEngine()
	yesBids := engine.GetDepth(marketID, models.OrderSideYes, "bids")
	yesAsks := engine.GetDepth(marketID, models.OrderSideYes, "asks")
	noBids := engine.GetDepth(marketID, models.OrderSideNo, "bids")
	noAsks := engine.GetDepth(marketID, models.OrderSideNo, "asks")
	lastPrice := engine.GetLastTradedPrice(marketID)
	spread := 0.0
	if len(yesBids) > 0 && len(yesAsks) > 0 {
		spread = yesAsks[0].Price - yesBids[0].Price
	}
	return &OrderbookSnapshot{
		MarketID:        marketID,
		YesBids:         yesBids,
		YesAsks:         yesAsks,
		NoBids:          noBids,
		NoAsks:          noAsks,
		LastTradedPrice: lastPrice,
		Spread:          spread,
		FetchedAt:       time.Now(),
	}, nil
}

func scheduleOrderExpiry(order *models.Order) {
	if order.ExpiresAt == nil {
		return
	}
	if delay := time.Until(*order.ExpiresAt); delay > 0 {
		time.Sleep(delay)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	current, err := GetOrderByID(ctx, order.ID)
	if err != nil {
		log.Printf("[Orders] Failed to fetch order %s for expiry check: %v", order.ID, err)
		return
	}
	if current.Status != models.OrderStatusOpen && current.Status != models.OrderStatusPartiallyFilled {
		return
	}
	if err := CancelOrder(ctx, order.ID, order.UserEmail); err != nil {
		log.Printf("[Orders] Failed to expire order %s: %v", order.ID, err)
	} else {
		log.Printf("[Orders] Order %s expired", order.ID)
	}
}

func getBestAsk(marketID string, side models.OrderSide) (float64, error) {
	asks := GetMatchingEngine().GetDepth(marketID, side, "asks")
	if len(asks) == 0 {
		return 0, nil
	}
	return asks[0].Price, nil
}

func estimateMarketOrderCost(marketID string, side models.OrderSide, quantity float64) (float64, error) {
	if quantity <= 0 {
		return 0, nil
	}
	asks := GetMatchingEngine().GetDepth(marketID, side, "asks")
	if len(asks) == 0 {
		return 0, nil
	}
	remainingQty := quantity
	totalCost := 0.0
	for _, level := range asks {
		if remainingQty <= 0 {
			break
		}
		fillQty := min64(remainingQty, level.Quantity)
		totalCost += fillQty * level.Price
		remainingQty -= fillQty
	}
	if totalCost == 0 {
		return 0, nil
	}
	return totalCost, nil
}
