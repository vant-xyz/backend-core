package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/vant-xyz/backend-code/models"
)

func GetBalanceByEmail(ctx context.Context, email string) (*models.Balance, error) {
	row := Pool.QueryRow(ctx, `
		SELECT id, email, usdc_sol, usdc_base, usdt_sol, usdg_sol, sol, eth_base,
		       naira, demo_usdc_sol, demo_sol, demo_naira, vnaira, locked_balance
		FROM balances WHERE email = $1
	`, email)

	var b models.Balance
	if err := row.Scan(
		&b.ID, &b.Email, &b.USDCSol, &b.USDCBase, &b.USDTSol, &b.USDGSol,
		&b.Sol, &b.ETHBase, &b.Naira, &b.DemoUSDCSol, &b.DemoSol,
		&b.DemoNaira, &b.VNaira, &b.LockedBalance,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("balance not found for %s", email)
		}
		return nil, fmt.Errorf("failed to get balance for %s: %w", email, err)
	}
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
// runs mutatorFn, and writes the result back atomically.
// Uses SELECT FOR UPDATE to prevent concurrent modifications.
func RunBalanceTransaction(ctx context.Context, userEmail string, mutatorFn func(*models.Balance) error) error {
	tx, err := Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("failed to begin balance transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, `
		SELECT id, email, usdc_sol, usdc_base, usdt_sol, usdg_sol, sol, eth_base,
		       naira, demo_usdc_sol, demo_sol, demo_naira, vnaira, locked_balance
		FROM balances WHERE email = $1
		FOR UPDATE
	`, userEmail)

	var b models.Balance
	if err := row.Scan(
		&b.ID, &b.Email, &b.USDCSol, &b.USDCBase, &b.USDTSol, &b.USDGSol,
		&b.Sol, &b.ETHBase, &b.Naira, &b.DemoUSDCSol, &b.DemoSol,
		&b.DemoNaira, &b.VNaira, &b.LockedBalance,
	); err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("balance not found for %s", userEmail)
		}
		return fmt.Errorf("failed to read balance for transaction: %w", err)
	}

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
			locked_balance = $12
		WHERE email = $13
	`,
		b.USDCSol, b.USDCBase, b.USDTSol, b.USDGSol,
		b.Sol, b.ETHBase, b.Naira, b.DemoUSDCSol, b.DemoSol,
		b.DemoNaira, b.VNaira, b.LockedBalance, userEmail,
	)
	if err != nil {
		return fmt.Errorf("failed to write balance after mutation: %w", err)
	}

	return tx.Commit(ctx)
}

func balanceFieldToColumn(field string) (string, error) {
	cols := map[string]string{
		"usdc_sol":       "usdc_sol",
		"usdc_base":      "usdc_base",
		"usdt_sol":       "usdt_sol",
		"usdg_sol":       "usdg_sol",
		"sol":            "sol",
		"eth_base":       "eth_base",
		"naira":          "naira",
		"demo_usdc_sol":  "demo_usdc_sol",
		"demo_sol":       "demo_sol",
		"demo_naira":     "demo_naira",
		"vnaira":         "vnaira",
		"locked_balance": "locked_balance",
	}
	col, ok := cols[field]
	if !ok {
		return "", fmt.Errorf("unknown balance field: %s", field)
	}
	return col, nil
}