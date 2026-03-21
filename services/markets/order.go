package markets

import (
	"context"
	"fmt"
	"log"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/models"
	"github.com/vant-xyz/backend-code/services"
	"github.com/vant-xyz/backend-code/utils"
	"google.golang.org/api/iterator"
)

const ordersCollection = "orders"

type PlaceOrderInput struct {
	UserEmail string
	MarketID  string
	Side      models.OrderSide
	Type      models.OrderType
	Price     float64
	Quantity  float64
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

	var lockedAmount float64
	if input.Type == models.OrderTypeLimit {
		lockedAmount = input.Price * input.Quantity
	} else {
		bestAsk, err := getBestAsk(ctx, input.MarketID, input.Side)
		if err != nil || bestAsk == 0 {
			return nil, fmt.Errorf("no liquidity available for market order on %s %s", input.MarketID, input.Side)
		}
		lockedAmount = bestAsk * input.Quantity
	}

	if err := services.LockBalance(ctx, input.UserEmail, lockedAmount, market.QuoteCurrency); err != nil {
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
		CreatedAt:     now,
		UpdatedAt:     now,
		ExpiresAt:     input.ExpiresAt,
	}

	if _, err := db.Client.Collection(ordersCollection).Doc(order.ID).Set(ctx, order); err != nil {
		if unlockErr := services.UnlockBalance(ctx, input.UserEmail, lockedAmount, market.QuoteCurrency); unlockErr != nil {
			log.Printf("[Orders] CRITICAL: failed to unlock balance after order save failure for %s: %v",
				input.UserEmail, unlockErr)
		}
		return nil, fmt.Errorf("failed to save order: %w", err)
	}

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

	market, err := GetMarketByID(ctx, order.MarketID)
	if err != nil {
		return fmt.Errorf("market not found for order %s: %w", orderID, err)
	}

	if err := updateOrderStatus(ctx, orderID, models.OrderStatusCancelled); err != nil {
		return fmt.Errorf("failed to update order status: %w", err)
	}

	GetMatchingEngine().Cancel(orderID)

	refundAmount := order.Price * order.RemainingQty
	if refundAmount > 0 {
		if err := services.UnlockBalance(ctx, userEmail, refundAmount, market.QuoteCurrency); err != nil {
			log.Printf("[Orders] CRITICAL: failed to unlock balance on cancel for order %s user %s: %v",
				orderID, userEmail, err)
		}
	}

	return nil
}

func GetOrderByID(ctx context.Context, orderID string) (*models.Order, error) {
	doc, err := db.Client.Collection(ordersCollection).Doc(orderID).Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get order %s: %w", orderID, err)
	}
	var order models.Order
	if err := doc.DataTo(&order); err != nil {
		return nil, fmt.Errorf("failed to deserialize order %s: %w", orderID, err)
	}
	return &order, nil
}

func GetUserOrders(ctx context.Context, userEmail string, marketID string) ([]models.Order, error) {
	var q firestore.Query

	if marketID != "" {
		q = db.Client.Collection(ordersCollection).
			Where("user_email", "==", userEmail).
			Where("market_id", "==", marketID).
			OrderBy("created_at", firestore.Desc)
	} else {
		q = db.Client.Collection(ordersCollection).
			Where("user_email", "==", userEmail).
			OrderBy("created_at", firestore.Desc)
	}

	return queryOrders(ctx, q)
}

func GetMarketOrders(ctx context.Context, marketID string, status models.OrderStatus) ([]models.Order, error) {
	q := db.Client.Collection(ordersCollection).
		Where("market_id", "==", marketID).
		Where("status", "==", string(status)).
		OrderBy("created_at", firestore.Asc)

	return queryOrders(ctx, q)
}

func GetOpenOrdersForMarket(ctx context.Context, marketID string) ([]models.Order, error) {
	open, err := GetMarketOrders(ctx, marketID, models.OrderStatusOpen)
	if err != nil {
		return nil, err
	}
	partial, err := GetMarketOrders(ctx, marketID, models.OrderStatusPartiallyFilled)
	if err != nil {
		return nil, err
	}
	return append(open, partial...), nil
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

func updateOrderStatus(ctx context.Context, orderID string, status models.OrderStatus) error {
	_, err := db.Client.Collection(ordersCollection).Doc(orderID).Update(ctx, []firestore.Update{
		{Path: "status", Value: string(status)},
		{Path: "updated_at", Value: time.Now()},
	})
	return err
}

func UpdateOrderFill(ctx context.Context, orderID string, filledQty, remainingQty float64, status models.OrderStatus) error {
	_, err := db.Client.Collection(ordersCollection).Doc(orderID).Update(ctx, []firestore.Update{
		{Path: "filled_qty", Value: filledQty},
		{Path: "remaining_qty", Value: remainingQty},
		{Path: "status", Value: string(status)},
		{Path: "updated_at", Value: time.Now()},
	})
	return err
}

func scheduleOrderExpiry(order *models.Order) {
	if order.ExpiresAt == nil {
		return
	}
	delay := time.Until(*order.ExpiresAt)
	if delay > 0 {
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

func getBestAsk(ctx context.Context, marketID string, side models.OrderSide) (float64, error) {
	engine := GetMatchingEngine()
	asks := engine.GetDepth(marketID, side, "asks")
	if len(asks) == 0 {
		return 0, nil
	}
	return asks[0].Price, nil
}

func queryOrders(ctx context.Context, q firestore.Query) ([]models.Order, error) {
	iter := q.Documents(ctx)
	var orders []models.Order
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error iterating orders: %w", err)
		}
		var order models.Order
		if err := doc.DataTo(&order); err != nil {
			log.Printf("[Orders] Failed to deserialize order %s: %v", doc.Ref.ID, err)
			continue
		}
		orders = append(orders, order)
	}
	return orders, nil
}