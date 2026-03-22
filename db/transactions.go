package db

import (
	"context"
	"fmt"

	"github.com/vant-xyz/backend-code/models"
)

func SaveTransaction(ctx context.Context, tx models.Transaction) error {
	_, err := Pool.Exec(ctx, `
		INSERT INTO transactions (id, user_email, amount, currency, nature, type, status, tx_hash, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO NOTHING
	`, tx.ID, tx.UserEmail, tx.Amount, tx.Currency, tx.Nature,
		tx.Type, tx.Status, tx.TxHash, tx.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to save transaction %s: %w", tx.ID, err)
	}
	return nil
}

func GetTransactionsByEmail(ctx context.Context, email string) ([]models.Transaction, error) {
	rows, err := Pool.Query(ctx, `
		SELECT id, user_email, amount, currency, nature, type, status, tx_hash, created_at
		FROM transactions
		WHERE user_email = $1
		ORDER BY created_at DESC
	`, email)
	if err != nil {
		return nil, fmt.Errorf("failed to query transactions for %s: %w", email, err)
	}
	defer rows.Close()

	var txs []models.Transaction
	for rows.Next() {
		var tx models.Transaction
		if err := rows.Scan(
			&tx.ID, &tx.UserEmail, &tx.Amount, &tx.Currency,
			&tx.Nature, &tx.Type, &tx.Status, &tx.TxHash, &tx.CreatedAt,
		); err != nil {
			continue
		}
		txs = append(txs, tx)
	}
	return txs, rows.Err()
}