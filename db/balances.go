package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/vant-xyz/backend-code/models"
)

func GetBalanceByEmail(ctx context.Context, email string) (*models.Balance, error) {
	row := Pool.QueryRow(ctx, `
		SELECT id, email, usdc_sol, usdc_base, usdt_sol, usdg_sol, sol, eth_base,
		       naira, demo_usdc_sol, demo_sol, demo_naira, vnaira,
		       locked_balance, locked_balance_real, locked_balance_demo
		FROM balances WHERE email = $1
	`, email)

	var b models.Balance
	if err := row.Scan(
		&b.ID, &b.Email, &b.USDCSol, &b.USDCBase, &b.USDTSol, &b.USDGSol,
		&b.Sol, &b.ETHBase, &b.Naira, &b.DemoUSDCSol, &b.DemoSol,
		&b.DemoNaira, &b.VNaira, &b.LockedBalance, &b.LockedReal, &b.LockedDemo,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("balance not found for %s", email)
		}
		return nil, fmt.Errorf("failed to get balance for %s: %w", email, err)
	}
	b.LockedBalance = b.LockedReal + b.LockedDemo
	return &b, nil
}

func UpdateBalance(ctx context.Context, email string, field string, amount float64) error {
	col, err := balanceFieldToColumn(field)
	if err != nil {
		return err
	}
	_, err = Pool.Exec(ctx,
		fmt.Sprintf(`UPDATE balances SET %s = %s + $1 WHERE email = $2`, col, col),
		amount, email,
	)
	return err
}

// DeductBalanceIfSufficient atomically subtracts amount from a balance field
// only when the current value is >= amount.
// Returns (true, nil) on successful deduction, (false, nil) when insufficient.
func DeductBalanceIfSufficient(ctx context.Context, email string, field string, amount float64) (bool, error) {
	if amount <= 0 {
		return false, fmt.Errorf("amount must be positive")
	}
	col, err := balanceFieldToColumn(field)
	if err != nil {
		return false, err
	}
	tag, err := Pool.Exec(
		ctx,
		fmt.Sprintf(`UPDATE balances SET %s = %s - $1 WHERE email = $2 AND %s >= $1`, col, col, col),
		amount,
		email,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func SetBalance(ctx context.Context, email string, field string, amount float64) error {
	col, err := balanceFieldToColumn(field)
	if err != nil {
		return err
	}
	_, err = Pool.Exec(ctx,
		fmt.Sprintf(`UPDATE balances SET %s = $1 WHERE email = $2`, col),
		amount, email,
	)
	return err
}

// RunBalanceTransaction reads the balance row inside a serializable transaction,
// runs mutatorFn, and writes the result back atomically. Retries on PostgreSQL
// serialization failures (SQLSTATE 40001) which occur under concurrent load.
func RunBalanceTransaction(ctx context.Context, userEmail string, mutatorFn func(*models.Balance) error) error {
	const maxAttempts = 5
	for attempt := range maxAttempts {
		err := runBalanceTxOnce(ctx, userEmail, mutatorFn)
		if err == nil {
			return nil
		}
		if isSerializationFailure(err) && attempt < maxAttempts-1 {
			time.Sleep(time.Duration(attempt+1) * 15 * time.Millisecond)
			continue
		}
		return err
	}
	return fmt.Errorf("balance transaction for %s failed after %d attempts", userEmail, maxAttempts)
}

func runBalanceTxOnce(ctx context.Context, userEmail string, mutatorFn func(*models.Balance) error) error {
	tx, err := Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("failed to begin balance transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, `
		SELECT id, email, usdc_sol, usdc_base, usdt_sol, usdg_sol, sol, eth_base,
		       naira, demo_usdc_sol, demo_sol, demo_naira, vnaira,
		       locked_balance, locked_balance_real, locked_balance_demo
		FROM balances WHERE email = $1
		FOR UPDATE
	`, userEmail)

	var b models.Balance
	if err := row.Scan(
		&b.ID, &b.Email, &b.USDCSol, &b.USDCBase, &b.USDTSol, &b.USDGSol,
		&b.Sol, &b.ETHBase, &b.Naira, &b.DemoUSDCSol, &b.DemoSol,
		&b.DemoNaira, &b.VNaira, &b.LockedBalance, &b.LockedReal, &b.LockedDemo,
	); err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("balance not found for %s", userEmail)
		}
		return fmt.Errorf("failed to read balance for transaction: %w", err)
	}

	b.LockedBalance = b.LockedReal + b.LockedDemo

	if err := mutatorFn(&b); err != nil {
		return err
	}

	_, err = tx.Exec(ctx, `
		UPDATE balances SET
			usdc_sol      = $1,
			usdc_base     = $2,
			usdt_sol      = $3,
			usdg_sol      = $4,
			sol           = $5,
			eth_base      = $6,
			naira         = $7,
			demo_usdc_sol = $8,
			demo_sol      = $9,
			demo_naira    = $10,
			vnaira        = $11,
			locked_balance = $12,
			locked_balance_real = $13,
			locked_balance_demo = $14
		WHERE email = $15
	`,
		b.USDCSol, b.USDCBase, b.USDTSol, b.USDGSol,
		b.Sol, b.ETHBase, b.Naira, b.DemoUSDCSol, b.DemoSol,
		b.DemoNaira, b.VNaira, b.LockedReal+b.LockedDemo, b.LockedReal, b.LockedDemo, userEmail,
	)
	if err != nil {
		return fmt.Errorf("failed to write balance after mutation: %w", err)
	}

	return tx.Commit(ctx)
}

func isSerializationFailure(err error) bool {
	if pgconn.SafeToRetry(err) {
		return true
	}
	type unwrapper interface{ Unwrap() error }
	for e := err; e != nil; {
		if pe, ok := e.(*pgconn.PgError); ok {
			return pe.Code == "40001"
		}
		if u, ok := e.(unwrapper); ok {
			e = u.Unwrap()
		} else {
			break
		}
	}
	return false
}

func balanceFieldToColumn(field string) (string, error) {
	cols := map[string]string{
		"usdc_sol":            "usdc_sol",
		"usdc_base":           "usdc_base",
		"usdt_sol":            "usdt_sol",
		"usdg_sol":            "usdg_sol",
		"sol":                 "sol",
		"eth_base":            "eth_base",
		"naira":               "naira",
		"demo_usdc_sol":       "demo_usdc_sol",
		"demo_sol":            "demo_sol",
		"demo_naira":          "demo_naira",
		"vnaira":              "vnaira",
		"locked_balance":      "locked_balance",
		"locked_balance_real": "locked_balance_real",
		"locked_balance_demo": "locked_balance_demo",
	}
	col, ok := cols[field]
	if !ok {
		return "", fmt.Errorf("unknown balance field: %s", field)
	}
	return col, nil
}
