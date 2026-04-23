package test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/services"
	"github.com/vant-xyz/backend-code/utils"
)

var dbOnce sync.Once

func initTestDB(t *testing.T) {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping DB integration tests")
	}
	dbOnce.Do(func() {
		db.Init(dbURL)
		if err := db.RunMigrations(context.Background()); err != nil {
			t.Fatalf("migrations failed: %v", err)
		}
	})
}

// createTestUser inserts a minimal user + balance row and registers cleanup.
// Returns the unique email address for the test user.
func createTestUser(t *testing.T, initialUSD float64) string {
	t.Helper()
	email := fmt.Sprintf("test_%s@test.vant.xyz", utils.RandomAlphanumeric(10))
	username := fmt.Sprintf("@test_%s", utils.RandomAlphanumeric(6))
	balanceID := fmt.Sprintf("BAL_%s", utils.RandomAlphanumeric(8))
	ctx := context.Background()

	_, err := db.Pool.Exec(ctx,
		`INSERT INTO users (email, password, username) VALUES ($1, $2, $3)`,
		email, "hashed_test_password", username,
	)
	if err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	_, err = db.Pool.Exec(ctx,
		`INSERT INTO balances (id, email, naira) VALUES ($1, $2, $3)`,
		balanceID, email, initialUSD,
	)
	if err != nil {
		t.Fatalf("insert test balance: %v", err)
	}

	t.Cleanup(func() {
		// balances CASCADE deletes when user is deleted
		db.Pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)
	})
	return email
}

// getBalance reads the current naira and locked_balance fields directly from DB.
func getBalance(t *testing.T, email string) (naira, locked float64) {
	t.Helper()
	err := db.Pool.QueryRow(context.Background(),
		`SELECT naira, locked_balance FROM balances WHERE email = $1`, email,
	).Scan(&naira, &locked)
	if err != nil {
		t.Fatalf("read balance for %s: %v", email, err)
	}
	return
}

// ── CreditBalance ─────────────────────────────────────────────────────────────

func TestCreditBalance_IncreasesUSDBalance(t *testing.T) {
	initTestDB(t)
	email := createTestUser(t, 100.0)

	if err := services.CreditBalance(context.Background(), email, 50.0, "USD"); err != nil {
		t.Fatalf("CreditBalance error: %v", err)
	}

	naira, _ := getBalance(t, email)
	if naira != 150.0 {
		t.Errorf("balance after credit = %.2f, want 150.00", naira)
	}
}

func TestCreditBalance_MultipleCreditsAccumulate(t *testing.T) {
	initTestDB(t)
	email := createTestUser(t, 0.0)

	for i := 0; i < 3; i++ {
		if err := services.CreditBalance(context.Background(), email, 100.0, "USD"); err != nil {
			t.Fatalf("CreditBalance error on call %d: %v", i+1, err)
		}
	}

	naira, _ := getBalance(t, email)
	if naira != 300.0 {
		t.Errorf("balance after 3×$100 credits = %.2f, want 300.00", naira)
	}
}

// ── LockBalance ───────────────────────────────────────────────────────────────

func TestLockBalance_MovesFromAvailableToLocked(t *testing.T) {
	initTestDB(t)
	email := createTestUser(t, 200.0)

	if err := services.LockBalance(context.Background(), email, 75.0, "USD"); err != nil {
		t.Fatalf("LockBalance error: %v", err)
	}

	naira, locked := getBalance(t, email)
	if naira != 125.0 {
		t.Errorf("available after lock = %.2f, want 125.00", naira)
	}
	if locked != 75.0 {
		t.Errorf("locked after lock = %.2f, want 75.00", locked)
	}
}

func TestLockBalance_InsufficientFunds_ReturnsError(t *testing.T) {
	initTestDB(t)
	email := createTestUser(t, 50.0)

	err := services.LockBalance(context.Background(), email, 100.0, "USD")
	if err == nil {
		t.Error("LockBalance with insufficient funds should return error")
	}

	naira, locked := getBalance(t, email)
	if naira != 50.0 || locked != 0 {
		t.Errorf("balance should be unchanged after failed lock: naira=%.2f locked=%.2f", naira, locked)
	}
}

func TestLockBalance_ExactBalance_Succeeds(t *testing.T) {
	initTestDB(t)
	email := createTestUser(t, 100.0)

	if err := services.LockBalance(context.Background(), email, 100.0, "USD"); err != nil {
		t.Fatalf("LockBalance of exact balance should succeed: %v", err)
	}

	naira, locked := getBalance(t, email)
	if naira != 0.0 {
		t.Errorf("available should be 0 after locking full balance, got %.2f", naira)
	}
	if locked != 100.0 {
		t.Errorf("locked should be 100, got %.2f", locked)
	}
}

// ── UnlockBalance ─────────────────────────────────────────────────────────────

func TestUnlockBalance_MovesFromLockedToAvailable(t *testing.T) {
	initTestDB(t)
	email := createTestUser(t, 200.0)

	if err := services.LockBalance(context.Background(), email, 100.0, "USD"); err != nil {
		t.Fatalf("LockBalance setup error: %v", err)
	}
	if err := services.UnlockBalance(context.Background(), email, 40.0, "USD"); err != nil {
		t.Fatalf("UnlockBalance error: %v", err)
	}

	naira, locked := getBalance(t, email)
	if naira != 140.0 {
		t.Errorf("available after unlock = %.2f, want 140.00", naira)
	}
	if locked != 60.0 {
		t.Errorf("locked after unlock = %.2f, want 60.00", locked)
	}
}

func TestUnlockBalance_MoreThanLocked_ReturnsError(t *testing.T) {
	initTestDB(t)
	email := createTestUser(t, 100.0)

	if err := services.LockBalance(context.Background(), email, 50.0, "USD"); err != nil {
		t.Fatalf("LockBalance setup error: %v", err)
	}
	err := services.UnlockBalance(context.Background(), email, 200.0, "USD")
	if err == nil {
		t.Error("UnlockBalance of more than locked should return error")
	}
}

// ── DeductLockedBalance ───────────────────────────────────────────────────────

func TestDeductLockedBalance_RemovesFromLockedWithoutReturning(t *testing.T) {
	initTestDB(t)
	email := createTestUser(t, 300.0)

	if err := services.LockBalance(context.Background(), email, 150.0, "USD"); err != nil {
		t.Fatalf("LockBalance setup error: %v", err)
	}
	if err := services.DeductLockedBalance(context.Background(), email, 60.0); err != nil {
		t.Fatalf("DeductLockedBalance error: %v", err)
	}

	naira, locked := getBalance(t, email)
	if naira != 150.0 {
		t.Errorf("available should be unchanged at 150.00, got %.2f", naira)
	}
	if locked != 90.0 {
		t.Errorf("locked after deduct = %.2f, want 90.00", locked)
	}
}

func TestDeductLockedBalance_MoreThanLocked_ReturnsError(t *testing.T) {
	initTestDB(t)
	email := createTestUser(t, 100.0)

	if err := services.LockBalance(context.Background(), email, 50.0, "USD"); err != nil {
		t.Fatalf("LockBalance setup error: %v", err)
	}
	err := services.DeductLockedBalance(context.Background(), email, 100.0)
	if err == nil {
		t.Error("DeductLockedBalance of more than locked should return error")
	}
}

// ── Full order lifecycle ──────────────────────────────────────────────────────

func TestBalanceLifecycle_LockDeductCredit(t *testing.T) {
	// Simulates: user places $60 order (lock), order fills at $60 (deduct locked),
	// user wins position and receives $100 payout (credit).
	initTestDB(t)
	email := createTestUser(t, 500.0)

	if err := services.LockBalance(context.Background(), email, 60.0, "USD"); err != nil {
		t.Fatalf("LockBalance: %v", err)
	}
	if err := services.DeductLockedBalance(context.Background(), email, 60.0); err != nil {
		t.Fatalf("DeductLockedBalance: %v", err)
	}
	if err := services.CreditBalance(context.Background(), email, 100.0, "USD"); err != nil {
		t.Fatalf("CreditBalance: %v", err)
	}

	naira, locked := getBalance(t, email)
	// Started with 500, locked 60 (available=440), deducted 60 (available=440, locked=0),
	// credited 100 (available=540).
	if naira != 540.0 {
		t.Errorf("final available = %.2f, want 540.00", naira)
	}
	if locked != 0 {
		t.Errorf("final locked = %.2f, want 0", locked)
	}
}
