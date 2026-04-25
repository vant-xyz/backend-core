package test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/models"
	marketsvc "github.com/vant-xyz/backend-code/services/markets"
	"github.com/vant-xyz/backend-code/utils"
)

var redisOnce sync.Once

func initTestRedis(t *testing.T) {
	t.Helper()
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		t.Skip("REDIS_URL not set — skipping matching engine integration tests")
	}
	redisOnce.Do(func() {
		db.InitRedis()
	})
}

func createTestMarket(t *testing.T) string {
	t.Helper()
	id := fmt.Sprintf("MKT_TEST_%s", utils.RandomAlphanumeric(8))
	now := time.Now()
	end := now.Add(10 * time.Minute)

	m := &models.Market{
		ID:              id,
		MarketType:      models.MarketTypeCAPPM,
		Status:          models.MarketStatusActive,
		QuoteCurrency:   "USD",
		Title:           "Test market",
		Description:     "Integration test market",
		StartTimeUTC:    now,
		EndTimeUTC:      end,
		DurationSeconds: 600,
		CreatedAt:       now,
	}
	if err := db.SaveMarket(context.Background(), m); err != nil {
		t.Fatalf("createTestMarket: %v", err)
	}
	t.Cleanup(func() {
		db.Pool.Exec(context.Background(), `DELETE FROM markets WHERE id = $1`, id)
	})
	return id
}

func getOrderStatus(t *testing.T, orderID string) (status string, filledQty, remainingQty float64) {
	t.Helper()
	err := db.Pool.QueryRow(context.Background(),
		`SELECT status, filled_qty, remaining_qty FROM orders WHERE id = $1`, orderID,
	).Scan(&status, &filledQty, &remainingQty)
	if err != nil {
		t.Fatalf("getOrderStatus %s: %v", orderID, err)
	}
	return
}

func getPositionCount(t *testing.T, email, marketID string) int {
	t.Helper()
	var n int
	db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM positions WHERE user_email = $1 AND market_id = $2 AND status = 'ACTIVE'`,
		email, marketID,
	).Scan(&n)
	return n
}

// ── Cross-match integration ───────────────────────────────────────────────────

func TestCrossMatch_BothOrdersFilled_InDB(t *testing.T) {
	initTestDB(t)
	initTestRedis(t)

	alice := createTestUser(t, 100.0)
	bob := createTestUser(t, 100.0)
	marketID := createTestMarket(t)

	ctx := context.Background()

	// Alice: YES @ 0.65 × 50 → cost $32.50
	aliceOrder, err := marketsvc.PlaceOrder(ctx, marketsvc.PlaceOrderInput{
		UserEmail: alice,
		MarketID:  marketID,
		Side:      models.OrderSideYes,
		Type:      models.OrderTypeLimit,
		Price:     0.65,
		Quantity:  50,
	})
	if err != nil {
		t.Fatalf("Alice PlaceOrder: %v", err)
	}

	// Bob: NO @ 0.35 × 50 → cost $17.50; prices sum to 1.00, should cross-match
	bobOrder, err := marketsvc.PlaceOrder(ctx, marketsvc.PlaceOrderInput{
		UserEmail: bob,
		MarketID:  marketID,
		Side:      models.OrderSideNo,
		Type:      models.OrderTypeLimit,
		Price:     0.35,
		Quantity:  50,
	})
	if err != nil {
		t.Fatalf("Bob PlaceOrder: %v", err)
	}

	// Give goroutines time to persist to DB
	time.Sleep(300 * time.Millisecond)

	aliceStatus, aliceFilled, aliceRemaining := getOrderStatus(t, aliceOrder.ID)
	bobStatus, bobFilled, bobRemaining := getOrderStatus(t, bobOrder.ID)

	if aliceStatus != "FILLED" {
		t.Errorf("Alice order status = %s, want FILLED", aliceStatus)
	}
	if aliceFilled != 50 {
		t.Errorf("Alice filled_qty = %.0f, want 50", aliceFilled)
	}
	if aliceRemaining != 0 {
		t.Errorf("Alice remaining_qty = %.0f, want 0", aliceRemaining)
	}

	if bobStatus != "FILLED" {
		t.Errorf("Bob order status = %s, want FILLED", bobStatus)
	}
	if bobFilled != 50 {
		t.Errorf("Bob filled_qty = %.0f, want 50", bobFilled)
	}
	if bobRemaining != 0 {
		t.Errorf("Bob remaining_qty = %.0f, want 0", bobRemaining)
	}
}

func TestCrossMatch_PositionsCreated_InDB(t *testing.T) {
	initTestDB(t)
	initTestRedis(t)

	alice := createTestUser(t, 100.0)
	bob := createTestUser(t, 100.0)
	marketID := createTestMarket(t)

	ctx := context.Background()

	if _, err := marketsvc.PlaceOrder(ctx, marketsvc.PlaceOrderInput{
		UserEmail: alice, MarketID: marketID,
		Side: models.OrderSideYes, Type: models.OrderTypeLimit,
		Price: 0.65, Quantity: 50,
	}); err != nil {
		t.Fatalf("Alice PlaceOrder: %v", err)
	}
	if _, err := marketsvc.PlaceOrder(ctx, marketsvc.PlaceOrderInput{
		UserEmail: bob, MarketID: marketID,
		Side: models.OrderSideNo, Type: models.OrderTypeLimit,
		Price: 0.35, Quantity: 50,
	}); err != nil {
		t.Fatalf("Bob PlaceOrder: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	if n := getPositionCount(t, alice, marketID); n != 1 {
		t.Errorf("Alice active positions = %d, want 1", n)
	}
	if n := getPositionCount(t, bob, marketID); n != 1 {
		t.Errorf("Bob active positions = %d, want 1", n)
	}
}

func TestCrossMatch_LockedBalanceDeducted_InDB(t *testing.T) {
	initTestDB(t)
	initTestRedis(t)

	alice := createTestUser(t, 100.0)
	bob := createTestUser(t, 100.0)
	marketID := createTestMarket(t)

	ctx := context.Background()

	if _, err := marketsvc.PlaceOrder(ctx, marketsvc.PlaceOrderInput{
		UserEmail: alice, MarketID: marketID,
		Side: models.OrderSideYes, Type: models.OrderTypeLimit,
		Price: 0.65, Quantity: 50,
	}); err != nil {
		t.Fatalf("Alice PlaceOrder: %v", err)
	}
	if _, err := marketsvc.PlaceOrder(ctx, marketsvc.PlaceOrderInput{
		UserEmail: bob, MarketID: marketID,
		Side: models.OrderSideNo, Type: models.OrderTypeLimit,
		Price: 0.35, Quantity: 50,
	}); err != nil {
		t.Fatalf("Bob PlaceOrder: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	// After cross-fill: locked balance is deducted (consumed, not returned)
	_, aliceLocked := getBalance(t, alice)
	_, bobLocked := getBalance(t, bob)

	if aliceLocked != 0 {
		t.Errorf("Alice locked_balance = %.2f after fill, want 0", aliceLocked)
	}
	if bobLocked != 0 {
		t.Errorf("Bob locked_balance = %.2f after fill, want 0", bobLocked)
	}
}

func TestCrossMatch_IncompatiblePrices_OrdersRest_NotFilled(t *testing.T) {
	initTestDB(t)
	initTestRedis(t)

	alice := createTestUser(t, 100.0)
	bob := createTestUser(t, 100.0)
	marketID := createTestMarket(t)

	ctx := context.Background()

	// Prices sum to 0.90 — no cross-match should occur
	aliceOrder, err := marketsvc.PlaceOrder(ctx, marketsvc.PlaceOrderInput{
		UserEmail: alice, MarketID: marketID,
		Side: models.OrderSideYes, Type: models.OrderTypeLimit,
		Price: 0.60, Quantity: 50,
	})
	if err != nil {
		t.Fatalf("Alice PlaceOrder: %v", err)
	}
	bobOrder, err := marketsvc.PlaceOrder(ctx, marketsvc.PlaceOrderInput{
		UserEmail: bob, MarketID: marketID,
		Side: models.OrderSideNo, Type: models.OrderTypeLimit,
		Price: 0.30, Quantity: 50,
	})
	if err != nil {
		t.Fatalf("Bob PlaceOrder: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	aliceStatus, _, _ := getOrderStatus(t, aliceOrder.ID)
	bobStatus, _, _ := getOrderStatus(t, bobOrder.ID)

	if aliceStatus != "OPEN" {
		t.Errorf("Alice order status = %s, want OPEN (prices sum to 0.90)", aliceStatus)
	}
	if bobStatus != "OPEN" {
		t.Errorf("Bob order status = %s, want OPEN (prices sum to 0.90)", bobStatus)
	}

	if n := getPositionCount(t, alice, marketID); n != 0 {
		t.Errorf("Alice should have no positions (no fill), got %d", n)
	}
}
