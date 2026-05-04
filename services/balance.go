package services

import (
	"context"
	"fmt"
	"math"

	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/models"
)

const balanceEpsilon = 1e-9

func roundCents(v float64) float64 {
	return math.Round(v*100) / 100
}

const (
	lockedBalanceField     = "locked_balance"
	lockedBalanceRealField = "locked_balance_real"
	lockedBalanceDemoField = "locked_balance_demo"
)

func LockBalance(ctx context.Context, userEmail string, amount float64, currency string) error {
	if amount <= 0 {
		return fmt.Errorf("lock amount must be positive, got %f", amount)
	}

	available, err := GetAvailableBalance(ctx, userEmail, currency)
	if err != nil {
		return fmt.Errorf("failed to read available balance: %w", err)
	}

	if available < amount {
		return fmt.Errorf("insufficient balance: available=%.2f requested=%.2f %s", available, amount, currency)
	}

	balanceField := currencyToField(currency)
	if balanceField == "" {
		return fmt.Errorf("unsupported quote currency: %s", currency)
	}
	lockedField := lockedFieldForCurrency(currency)

	return db.RunBalanceTransaction(ctx, userEmail, func(balance *models.Balance) error {
		available := balanceFieldValue(balance, balanceField)
		if available < amount {
			return fmt.Errorf("insufficient balance during transaction: available=%.2f requested=%.2f", available, amount)
		}
		setBalanceField(balance, balanceField, available-amount)
		setBalanceField(balance, lockedField, balanceFieldValue(balance, lockedField)+amount)
		return nil
	})
}

func UnlockBalance(ctx context.Context, userEmail string, amount float64, currency string) error {
	if amount <= 0 {
		return fmt.Errorf("unlock amount must be positive, got %f", amount)
	}

	lockedField := lockedFieldForCurrency(currency)

	return db.RunBalanceTransaction(ctx, userEmail, func(balance *models.Balance) error {
		lockedVal := balanceFieldValue(balance, lockedField)
		if lockedVal+balanceEpsilon < amount {
			return fmt.Errorf("locked balance %.2f is less than unlock amount %.2f", lockedVal, amount)
		}
		balanceField := currencyToField(currency)
		if balanceField == "" {
			return fmt.Errorf("unsupported quote currency: %s", currency)
		}
		actual := math.Min(amount, lockedVal)
		current := balanceFieldValue(balance, balanceField)
		setBalanceField(balance, balanceField, roundCents(current+actual))
		setBalanceField(balance, lockedField, roundCents(lockedVal-actual))
		return nil
	})
}

func DeductLockedBalance(ctx context.Context, userEmail string, amount float64) error {
	return DeductLockedBalanceByCurrency(ctx, userEmail, amount, "USD")
}

func DeductLockedBalanceByCurrency(ctx context.Context, userEmail string, amount float64, currency string) error {
	if amount <= 0 {
		return fmt.Errorf("deduct amount must be positive, got %f", amount)
	}

	lockedField := lockedFieldForCurrency(currency)

	return db.RunBalanceTransaction(ctx, userEmail, func(balance *models.Balance) error {
		lockedVal := balanceFieldValue(balance, lockedField)
		if lockedVal+balanceEpsilon < amount {
			return fmt.Errorf("locked balance %.2f is less than deduct amount %.2f", lockedVal, amount)
		}
		actual := math.Min(amount, lockedVal)
		setBalanceField(balance, lockedField, roundCents(lockedVal-actual))
		return nil
	})
}

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

func GetLockedBalance(ctx context.Context, userEmail string) (float64, error) {
	balance, err := db.GetBalanceByEmail(ctx, userEmail)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch balance for %s: %w", userEmail, err)
	}
	return balance.LockedReal + balance.LockedDemo, nil
}

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
	return available + lockedBalanceForCurrency(balance, currency), nil
}

func currencyToField(currency string) string {
	switch currency {
	case "USD", "NGN":
		return "naira"
	case "USD_DEMO", "NGN_DEMO":
		return "demo_naira"
	default:
		return ""
	}
}

func lockedFieldForCurrency(currency string) string {
	switch currency {
	case "USD", "NGN":
		return lockedBalanceRealField
	case "USD_DEMO", "NGN_DEMO":
		return lockedBalanceDemoField
	default:
		return lockedBalanceField
	}
}

func balanceFieldValue(b *models.Balance, field string) float64 {
	switch field {
	case "naira":
		return b.Naira
	case "demo_naira":
		return b.DemoNaira
	case lockedBalanceField:
		if b.LockedReal == 0 && b.LockedDemo == 0 && b.LockedBalance != 0 {
			return b.LockedBalance
		}
		return b.LockedReal + b.LockedDemo
	case lockedBalanceRealField:
		return b.LockedReal
	case lockedBalanceDemoField:
		return b.LockedDemo
	default:
		return 0
	}
}

func setBalanceField(b *models.Balance, field string, value float64) {
	switch field {
	case "naira":
		b.Naira = value
	case "demo_naira":
		b.DemoNaira = value
	case lockedBalanceField:
		b.LockedBalance = value
		b.LockedReal = value
		b.LockedDemo = 0
	case lockedBalanceRealField:
		b.LockedReal = value
	case lockedBalanceDemoField:
		b.LockedDemo = value
	}
	b.LockedBalance = b.LockedReal + b.LockedDemo
}

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
	locked = lockedBalanceForCurrency(balance, currency)
	total = available + locked
	return available, locked, total, nil
}

func lockedBalanceForCurrency(balance *models.Balance, currency string) float64 {
	switch currency {
	case "USD", "NGN":
		return balance.LockedReal
	case "USD_DEMO", "NGN_DEMO":
		return balance.LockedDemo
	default:
		return balance.LockedReal + balance.LockedDemo
	}
}
