package services

import (
	"context"
	"fmt"

	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/models"
)

const lockedBalanceField = "locked_balance"

// LockBalance moves amount from available balance into locked balance,
// reserving it for an open limit order. Returns an error if the user
// does not have sufficient available balance.
func LockBalance(ctx context.Context, userEmail string, amount float64, currency string) error {
	if amount <= 0 {
		return fmt.Errorf("lock amount must be positive, got %f", amount)
	}

	available, err := GetAvailableBalance(ctx, userEmail, currency)
	if err != nil {
		return fmt.Errorf("failed to read available balance: %w", err)
	}

	if available < amount {
		return fmt.Errorf("insufficient balance: available=%.2f requested=%.2f %s",
			available, amount, currency)
	}

	balanceField := currencyToField(currency)
	if balanceField == "" {
		return fmt.Errorf("unsupported quote currency: %s", currency)
	}

	return db.RunBalanceTransaction(ctx, userEmail, func(balance *models.Balance) error {
		available := balanceFieldValue(balance, balanceField)
		if available < amount {
			return fmt.Errorf("insufficient balance during transaction: available=%.2f requested=%.2f",
				available, amount)
		}
		setBalanceField(balance, balanceField, available-amount)
		setBalanceField(balance, lockedBalanceField, balance.LockedBalance+amount)
		return nil
	})
}

// UnlockBalance releases amount from locked balance back into available
// balance. Called when an order is cancelled or expires.
func UnlockBalance(ctx context.Context, userEmail string, amount float64, currency string) error {
	if amount <= 0 {
		return fmt.Errorf("unlock amount must be positive, got %f", amount)
	}

	return db.RunBalanceTransaction(ctx, userEmail, func(balance *models.Balance) error {
		if balance.LockedBalance < amount {
			return fmt.Errorf("locked balance %.2f is less than unlock amount %.2f",
				balance.LockedBalance, amount)
		}
		balanceField := currencyToField(currency)
		if balanceField == "" {
			return fmt.Errorf("unsupported quote currency: %s", currency)
		}
		current := balanceFieldValue(balance, balanceField)
		setBalanceField(balance, balanceField, current+amount)
		setBalanceField(balance, lockedBalanceField, balance.LockedBalance-amount)
		return nil
	})
}

// DeductLockedBalance removes amount directly from locked balance without
// returning it to available. Called when a locked order is matched and filled —
// the funds leave the user's balance entirely to pay for the position.
func DeductLockedBalance(ctx context.Context, userEmail string, amount float64) error {
	if amount <= 0 {
		return fmt.Errorf("deduct amount must be positive, got %f", amount)
	}

	return db.RunBalanceTransaction(ctx, userEmail, func(balance *models.Balance) error {
		if balance.LockedBalance < amount {
			return fmt.Errorf("locked balance %.2f is less than deduct amount %.2f",
				balance.LockedBalance, amount)
		}
		setBalanceField(balance, lockedBalanceField, balance.LockedBalance-amount)
		return nil
	})
}

// CreditBalance adds amount to the user's available balance for the given
// currency. Used for payouts and order refunds.
func CreditBalance(ctx context.Context, userEmail string, amount float64, currency string) error {
	if amount <= 0 {
		return fmt.Errorf("credit amount must be positive, got %f", amount)
	}

	balanceField := currencyToField(currency)
	if balanceField == "" {
		return fmt.Errorf("unsupported quote currency: %s", currency)
	}

	return db.RunBalanceTransaction(ctx, userEmail, func(balance *models.Balance) error {
		current := balanceFieldValue(balance, balanceField)
		setBalanceField(balance, balanceField, current+amount)
		return nil
	})
}

// GetAvailableBalance returns the spendable balance for a currency —
// excludes locked funds.
func GetAvailableBalance(ctx context.Context, userEmail string, currency string) (float64, error) {
	balance, err := db.GetBalanceByEmail(ctx, userEmail)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch balance for %s: %w", userEmail, err)
	}

	balanceField := currencyToField(currency)
	if balanceField == "" {
		return 0, fmt.Errorf("unsupported quote currency: %s", currency)
	}

	return balanceFieldValue(balance, balanceField), nil
}

// GetLockedBalance returns the total amount currently locked in open orders.
func GetLockedBalance(ctx context.Context, userEmail string) (float64, error) {
	balance, err := db.GetBalanceByEmail(ctx, userEmail)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch balance for %s: %w", userEmail, err)
	}
	return balance.LockedBalance, nil
}

// GetTotalBalance returns available + locked for a currency.
// Does not include unrealised position value.
func GetTotalBalance(ctx context.Context, userEmail string, currency string) (float64, error) {
	balance, err := db.GetBalanceByEmail(ctx, userEmail)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch balance for %s: %w", userEmail, err)
	}

	balanceField := currencyToField(currency)
	if balanceField == "" {
		return 0, fmt.Errorf("unsupported quote currency: %s", currency)
	}

	available := balanceFieldValue(balance, balanceField)
	return available + balance.LockedBalance, nil
}

// currencyToField maps a quote currency code to its Firestore balance field.
// Extend this map as new currencies are supported.
func currencyToField(currency string) string {
	switch currency {
	case "NGN":
		return "naira"
	case "NGN_DEMO":
		return "demo_naira"
	default:
		return ""
	}
}

// balanceFieldValue reads the named field from a Balance struct.
func balanceFieldValue(b *models.Balance, field string) float64 {
	switch field {
	case "naira":
		return b.Naira
	case "demo_naira":
		return b.DemoNaira
	case lockedBalanceField:
		return b.LockedBalance
	default:
		return 0
	}
}

// setBalanceField writes a value to the named field on a Balance struct.
func setBalanceField(b *models.Balance, field string, value float64) {
	switch field {
	case "naira":
		b.Naira = value
	case "demo_naira":
		b.DemoNaira = value
	case lockedBalanceField:
		b.LockedBalance = value
	}
}

// GetUserOrdersBalanceSummary returns a breakdown of available, locked, and
// total balance for display in the user's wallet.
func GetUserBalanceSummary(ctx context.Context, userEmail string, currency string) (available, locked, total float64, err error) {
	balance, err := db.GetBalanceByEmail(ctx, userEmail)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to fetch balance for %s: %w", userEmail, err)
	}

	balanceField := currencyToField(currency)
	if balanceField == "" {
		return 0, 0, 0, fmt.Errorf("unsupported quote currency: %s", currency)
	}

	available = balanceFieldValue(balance, balanceField)
	locked = balance.LockedBalance
	total = available + locked
	return available, locked, total, nil
}