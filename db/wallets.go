package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/vant-xyz/backend-code/models"
)

func GetWalletByEmail(ctx context.Context, email string) (*models.Wallet, error) {
	row := Pool.QueryRow(ctx, `
		SELECT account_id, email, sol_public_key, sol_private_key,
		       base_public_key, base_private_key, naira_account_number
		FROM wallets WHERE email = $1
	`, email)

	var w models.Wallet
	if err := row.Scan(
		&w.AccountID, &w.Email, &w.SolPublicKey, &w.SolPrivateKey,
		&w.BasePublicKey, &w.BasePrivateKey, &w.NairaAccountNumber,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("wallet not found for %s", email)
		}
		return nil, fmt.Errorf("failed to get wallet for %s: %w", email, err)
	}
	return &w, nil
}

func GetAllWallets(ctx context.Context) ([]models.Wallet, error) {
	rows, err := Pool.Query(ctx, `
		SELECT account_id, email, sol_public_key, sol_private_key,
		       base_public_key, base_private_key, naira_account_number
		FROM wallets
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query wallets: %w", err)
	}
	defer rows.Close()

	wallets := make([]models.Wallet, 0)
	for rows.Next() {
		var w models.Wallet
		if err := rows.Scan(
			&w.AccountID, &w.Email, &w.SolPublicKey, &w.SolPrivateKey,
			&w.BasePublicKey, &w.BasePrivateKey, &w.NairaAccountNumber,
		); err != nil {
			continue
		}
		wallets = append(wallets, w)
	}
	return wallets, rows.Err()
}