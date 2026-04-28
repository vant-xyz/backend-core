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
	time.Sleep(600 * time.Millisecond)

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

	time.Sleep(600 * time.Millisecond)

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

	time.Sleep(600 * time.Millisecond)

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

	time.Sleep(600 * time.Millisecond)

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

// ── Partial fill ──────────────────────────────────────────────────────────────

func TestPartialFill_SmallNoFillsPartOfLargeYes(t *testing.T) {
	// YES 100 × NO 50 at crossing prices → NO fully filled, YES partially filled
	initTestDB(t)
	initTestRedis(t)

	alice := createTestUser(t, 200.0)
	bob := createTestUser(t, 100.0)
	marketID := createTestMarket(t)

	ctx := context.Background()

	aliceOrder, err := marketsvc.PlaceOrder(ctx, marketsvc.PlaceOrderInput{
		UserEmail: alice, MarketID: marketID,
		Side: models.OrderSideYes, Type: models.OrderTypeLimit,
		Price: 0.65, Quantity: 100,
	})
	if err != nil {
		t.Fatalf("Alice PlaceOrder: %v", err)
	}

	bobOrder, err := marketsvc.PlaceOrder(ctx, marketsvc.PlaceOrderInput{
		UserEmail: bob, MarketID: marketID,
		Side: models.OrderSideNo, Type: models.OrderTypeLimit,
		Price: 0.35, Quantity: 50,
	})
	if err != nil {
		t.Fatalf("Bob PlaceOrder: %v", err)
	}

	time.Sleep(600 * time.Millisecond)

	aliceStatus, aliceFilled, aliceRemaining := getOrderStatus(t, aliceOrder.ID)
	bobStatus, bobFilled, bobRemaining := getOrderStatus(t, bobOrder.ID)

	if aliceStatus != "PARTIALLY_FILLED" {
		t.Errorf("Alice status = %s, want PARTIALLY_FILLED", aliceStatus)
	}
	if aliceFilled != 50 {
		t.Errorf("Alice filled_qty = %.0f, want 50", aliceFilled)
	}
	if aliceRemaining != 50 {
		t.Errorf("Alice remaining_qty = %.0f, want 50", aliceRemaining)
	}

	if bobStatus != "FILLED" {
		t.Errorf("Bob status = %s, want FILLED", bobStatus)
	}
	if bobFilled != 50 {
		t.Errorf("Bob filled_qty = %.0f, want 50", bobFilled)
	}
	if bobRemaining != 0 {
		t.Errorf("Bob remaining_qty = %.0f, want 0", bobRemaining)
	}
}

func TestPartialFill_LockedBalanceReflectsUnfilledPortion(t *testing.T) {
	// After partial fill: alice's locked = price × remaining_qty only
	initTestDB(t)
	initTestRedis(t)

	alice := createTestUser(t, 200.0)
	bob := createTestUser(t, 100.0)
	marketID := createTestMarket(t)

	ctx := context.Background()

	if _, err := marketsvc.PlaceOrder(ctx, marketsvc.PlaceOrderInput{
		UserEmail: alice, MarketID: marketID,
		Side: models.OrderSideYes, Type: models.OrderTypeLimit,
		Price: 0.65, Quantity: 100,
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

	time.Sleep(600 * time.Millisecond)

	_, aliceLocked := getBalance(t, alice)
	_, bobLocked := getBalance(t, bob)

	// Alice locked 100×0.65=$65, 50 filled → deducted $32.50 → remaining locked=$32.50
	if aliceLocked != 32.50 {
		t.Errorf("Alice locked after partial fill = %.2f, want 32.50", aliceLocked)
	}
	// Bob fully filled — locked fully deducted
	if bobLocked != 0 {
		t.Errorf("Bob locked after full fill = %.2f, want 0", bobLocked)
	}
}

// ── Cancel order ──────────────────────────────────────────────────────────────

func TestCancelOrder_OpenOrder_ReturnsLockedBalance(t *testing.T) {
	initTestDB(t)
	initTestRedis(t)

	alice := createTestUser(t, 100.0)
	marketID := createTestMarket(t)

	ctx := context.Background()

	order, err := marketsvc.PlaceOrder(ctx, marketsvc.PlaceOrderInput{
		UserEmail: alice, MarketID: marketID,
		Side: models.OrderSideYes, Type: models.OrderTypeLimit,
		Price: 0.60, Quantity: 50, // locks $30
	})
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}

	_, lockedAfterPlace := getBalance(t, alice)
	if lockedAfterPlace != 30.0 {
		t.Errorf("locked after place = %.2f, want 30.00", lockedAfterPlace)
	}

	time.Sleep(200 * time.Millisecond)

	if err := marketsvc.CancelOrder(ctx, order.ID, alice); err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}

	naira, lockedAfterCancel := getBalance(t, alice)
	if lockedAfterCancel != 0 {
		t.Errorf("locked after cancel = %.2f, want 0", lockedAfterCancel)
	}
	if naira != 100.0 {
		t.Errorf("available after cancel = %.2f, want 100.00 (full refund)", naira)
	}

	status, _, _ := getOrderStatus(t, order.ID)
	if status != "CANCELLED" {
		t.Errorf("order status = %s, want CANCELLED", status)
	}
}

func TestCancelOrder_PartiallyFilled_RefundsRemainingOnly(t *testing.T) {
	// Place YES 100, cross-fill 50, cancel remainder → refund = 50×0.65 = $32.50
	initTestDB(t)
	initTestRedis(t)

	alice := createTestUser(t, 200.0)
	bob := createTestUser(t, 100.0)
	marketID := createTestMarket(t)

	ctx := context.Background()

	aliceOrder, err := marketsvc.PlaceOrder(ctx, marketsvc.PlaceOrderInput{
		UserEmail: alice, MarketID: marketID,
		Side: models.OrderSideYes, Type: models.OrderTypeLimit,
		Price: 0.65, Quantity: 100,
	})
	if err != nil {
		t.Fatalf("Alice PlaceOrder: %v", err)
	}
	if _, err := marketsvc.PlaceOrder(ctx, marketsvc.PlaceOrderInput{
		UserEmail: bob, MarketID: marketID,
		Side: models.OrderSideNo, Type: models.OrderTypeLimit,
		Price: 0.35, Quantity: 50,
	}); err != nil {
		t.Fatalf("Bob PlaceOrder: %v", err)
	}

	time.Sleep(600 * time.Millisecond)

	if err := marketsvc.CancelOrder(ctx, aliceOrder.ID, alice); err != nil {
		t.Fatalf("CancelOrder after partial fill: %v", err)
	}

	naira, locked := getBalance(t, alice)
	// Started $200, locked $65 (100×0.65), 50 filled → $32.50 deducted.
	// Cancel refunds remaining $32.50 → available = $200 - $32.50 = $167.50.
	if locked != 0 {
		t.Errorf("locked after cancel = %.2f, want 0", locked)
	}
	if naira != 167.50 {
		t.Errorf("available after partial fill + cancel = %.2f, want 167.50", naira)
	}

	status, _, _ := getOrderStatus(t, aliceOrder.ID)
	if status != "CANCELLED" {
		t.Errorf("order status = %s, want CANCELLED", status)
	}
}

func TestCancelOrder_AlreadyFilled_ReturnsError(t *testing.T) {
	initTestDB(t)
	initTestRedis(t)

	alice := createTestUser(t, 100.0)
	bob := createTestUser(t, 100.0)
	marketID := createTestMarket(t)

	ctx := context.Background()

	aliceOrder, err := marketsvc.PlaceOrder(ctx, marketsvc.PlaceOrderInput{
		UserEmail: alice, MarketID: marketID,
		Side: models.OrderSideYes, Type: models.OrderTypeLimit,
		Price: 0.65, Quantity: 50,
	})
	if err != nil {
		t.Fatalf("Alice PlaceOrder: %v", err)
	}
	if _, err := marketsvc.PlaceOrder(ctx, marketsvc.PlaceOrderInput{
		UserEmail: bob, MarketID: marketID,
		Side: models.OrderSideNo, Type: models.OrderTypeLimit,
		Price: 0.35, Quantity: 50,
	}); err != nil {
		t.Fatalf("Bob PlaceOrder: %v", err)
	}

	time.Sleep(600 * time.Millisecond)

	if err := marketsvc.CancelOrder(ctx, aliceOrder.ID, alice); err == nil {
		t.Error("CancelOrder on FILLED order should return error")
	}
}

// ── Placement validation ──────────────────────────────────────────────────────

func TestPlaceOrder_InsufficientBalance_ReturnsError(t *testing.T) {
	initTestDB(t)
	initTestRedis(t)

	alice := createTestUser(t, 10.0)
	marketID := createTestMarket(t)

	ctx := context.Background()

	// Order cost = 0.60 × 50 = $30 — exceeds $10 balance
	_, err := marketsvc.PlaceOrder(ctx, marketsvc.PlaceOrderInput{
		UserEmail: alice, MarketID: marketID,
		Side: models.OrderSideYes, Type: models.OrderTypeLimit,
		Price: 0.60, Quantity: 50,
	})
	if err == nil {
		t.Error("PlaceOrder with insufficient balance should return error")
	}

	_, locked := getBalance(t, alice)
	if locked != 0 {
		t.Errorf("locked after failed order = %.2f, want 0", locked)
	}
}

func TestPlaceOrder_WrongOwner_CancelFails(t *testing.T) {
	initTestDB(t)
	initTestRedis(t)

	alice := createTestUser(t, 100.0)
	eve := createTestUser(t, 100.0)
	marketID := createTestMarket(t)

	ctx := context.Background()

	order, err := marketsvc.PlaceOrder(ctx, marketsvc.PlaceOrderInput{
		UserEmail: alice, MarketID: marketID,
		Side: models.OrderSideYes, Type: models.OrderTypeLimit,
		Price: 0.60, Quantity: 10,
	})
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}

	if err := marketsvc.CancelOrder(ctx, order.ID, eve); err == nil {
		t.Error("CancelOrder by non-owner should return error")
	}
}

// ── Multi-order fill ──────────────────────────────────────────────────────────

func TestMultiOrderFill_OneNoFillsMultipleYes(t *testing.T) {
	// Two YES orders (50 each) rest in book. One NO 100 arrives and fills both.
	initTestDB(t)
	initTestRedis(t)

	alice := createTestUser(t, 100.0)
	bob := createTestUser(t, 100.0)
	charlie := createTestUser(t, 100.0)
	marketID := createTestMarket(t)

	ctx := context.Background()

	aliceOrder, err := marketsvc.PlaceOrder(ctx, marketsvc.PlaceOrderInput{
		UserEmail: alice, MarketID: marketID,
		Side: models.OrderSideYes, Type: models.OrderTypeLimit,
		Price: 0.65, Quantity: 50,
	})
	if err != nil {
		t.Fatalf("Alice PlaceOrder: %v", err)
	}
	bobOrder, err := marketsvc.PlaceOrder(ctx, marketsvc.PlaceOrderInput{
		UserEmail: bob, MarketID: marketID,
		Side: models.OrderSideYes, Type: models.OrderTypeLimit,
		Price: 0.60, Quantity: 50,
	})
	if err != nil {
		t.Fatalf("Bob PlaceOrder: %v", err)
	}

	// Charlie: NO 100 @ 0.35 — crosses both YES bids (0.65+0.35≥1, 0.60+0.35<1)
	// Only alice's YES (0.65) crosses; bob's YES (0.60) does not (0.60+0.35=0.95<1.00)
	charlieOrder, err := marketsvc.PlaceOrder(ctx, marketsvc.PlaceOrderInput{
		UserEmail: charlie, MarketID: marketID,
		Side: models.OrderSideNo, Type: models.OrderTypeLimit,
		Price: 0.35, Quantity: 100,
	})
	if err != nil {
		t.Fatalf("Charlie PlaceOrder: %v", err)
	}

	time.Sleep(600 * time.Millisecond)

	aliceStatus, aliceFilled, _ := getOrderStatus(t, aliceOrder.ID)
	bobStatus, _, _ := getOrderStatus(t, bobOrder.ID)
	charlieStatus, charlieFilled, charlieRemaining := getOrderStatus(t, charlieOrder.ID)

	// Alice YES@0.65 + Charlie NO@0.35 = 1.00 → fills 50
	if aliceStatus != "FILLED" {
		t.Errorf("Alice status = %s, want FILLED", aliceStatus)
	}
	if aliceFilled != 50 {
		t.Errorf("Alice filled_qty = %.0f, want 50", aliceFilled)
	}

	// Bob YES@0.60 + Charlie NO@0.35 = 0.95 < 1.00 → no cross-match
	if bobStatus != "OPEN" {
		t.Errorf("Bob status = %s, want OPEN (0.60+0.35=0.95<1.00)", bobStatus)
	}

	// Charlie filled 50 (against alice only), 50 remaining
	if charlieStatus != "PARTIALLY_FILLED" {
		t.Errorf("Charlie status = %s, want PARTIALLY_FILLED", charlieStatus)
	}
	if charlieFilled != 50 {
		t.Errorf("Charlie filled_qty = %.0f, want 50", charlieFilled)
	}
	if charlieRemaining != 50 {
		t.Errorf("Charlie remaining_qty = %.0f, want 50", charlieRemaining)
	}
}

func TestClosePosition_FullClose_SetsClosedAndCreditsBalance(t *testing.T) {
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

	time.Sleep(600 * time.Millisecond)

	positions, err := marketsvc.GetUserPositions(ctx, alice, marketID)
	if err != nil || len(positions) == 0 {
		t.Fatalf("GetUserPositions: %v len=%d", err, len(positions))
	}
	pos := positions[0]
	if pos.Status != models.PositionStatusActive {
		t.Fatalf("position status=%s, want ACTIVE", pos.Status)
	}
	nairaBefore, _ := getBalance(t, alice)

	updated, proceeds, err := marketsvc.ClosePosition(ctx, marketsvc.ClosePositionInput{
		PositionID: pos.ID,
		MarketID:   marketID,
		UserEmail:  alice,
	})
	if err != nil {
		t.Fatalf("ClosePosition: %v", err)
	}
	if proceeds <= 0 {
		t.Fatalf("proceeds=%.2f, want > 0", proceeds)
	}
	if updated.Status != models.PositionStatusClosed {
		t.Fatalf("updated status=%s, want CLOSED", updated.Status)
	}
	if updated.Shares != 0 {
		t.Fatalf("updated shares=%.2f, want 0", updated.Shares)
	}

	var status string
	var shares float64
	if err := db.Pool.QueryRow(ctx, `SELECT status, shares FROM positions WHERE id = $1`, pos.ID).Scan(&status, &shares); err != nil {
		t.Fatalf("position row read: %v", err)
	}
	if status != string(models.PositionStatusClosed) {
		t.Fatalf("db status=%s, want %s", status, models.PositionStatusClosed)
	}
	if shares != 0 {
		t.Fatalf("db shares=%.2f, want 0", shares)
	}

	nairaAfter, _ := getBalance(t, alice)
	if nairaAfter <= nairaBefore {
		t.Fatalf("naira after close %.2f should be greater than before %.2f", nairaAfter, nairaBefore)
	}
}

func TestClosePosition_PartialClose_KeepsActiveAndReducesShares(t *testing.T) {
	initTestDB(t)
	initTestRedis(t)

	alice := createTestUser(t, 200.0)
	bob := createTestUser(t, 200.0)
	marketID := createTestMarket(t)
	ctx := context.Background()

	if _, err := marketsvc.PlaceOrder(ctx, marketsvc.PlaceOrderInput{
		UserEmail: alice, MarketID: marketID,
		Side: models.OrderSideYes, Type: models.OrderTypeLimit,
		Price: 0.65, Quantity: 100,
	}); err != nil {
		t.Fatalf("Alice PlaceOrder: %v", err)
	}
	if _, err := marketsvc.PlaceOrder(ctx, marketsvc.PlaceOrderInput{
		UserEmail: bob, MarketID: marketID,
		Side: models.OrderSideNo, Type: models.OrderTypeLimit,
		Price: 0.35, Quantity: 100,
	}); err != nil {
		t.Fatalf("Bob PlaceOrder: %v", err)
	}

	time.Sleep(600 * time.Millisecond)

	positions, err := marketsvc.GetUserPositions(ctx, alice, marketID)
	if err != nil || len(positions) == 0 {
		t.Fatalf("GetUserPositions: %v len=%d", err, len(positions))
	}
	pos := positions[0]
	startShares := pos.Shares
	if startShares < 40 {
		t.Fatalf("unexpected start shares %.2f", startShares)
	}

	updated, proceeds, err := marketsvc.ClosePosition(ctx, marketsvc.ClosePositionInput{
		PositionID: pos.ID,
		MarketID:   marketID,
		UserEmail:  alice,
		Shares:     40,
	})
	if err != nil {
		t.Fatalf("ClosePosition partial: %v", err)
	}
	if proceeds <= 0 {
		t.Fatalf("proceeds=%.2f, want > 0", proceeds)
	}
	if updated.Status != models.PositionStatusActive {
		t.Fatalf("updated status=%s, want ACTIVE", updated.Status)
	}
	if updated.Shares != startShares-40 {
		t.Fatalf("updated shares=%.2f, want %.2f", updated.Shares, startShares-40)
	}

	var status string
	var shares float64
	if err := db.Pool.QueryRow(ctx, `SELECT status, shares FROM positions WHERE id = $1`, pos.ID).Scan(&status, &shares); err != nil {
		t.Fatalf("position row read: %v", err)
	}
	if status != string(models.PositionStatusActive) {
		t.Fatalf("db status=%s, want %s", status, models.PositionStatusActive)
	}
	if shares != startShares-40 {
		t.Fatalf("db shares=%.2f, want %.2f", shares, startShares-40)
	}
}
