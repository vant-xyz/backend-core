package db

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
	"github.com/vant-xyz/backend-code/models"
	"google.golang.org/api/iterator"
)

// RunBalanceTransaction fetches a user's balance document, runs mutatorFn on
// it inside a Firestore transaction, and writes the result back atomically.
// This prevents race conditions when multiple goroutines modify the same
// balance simultaneously (e.g. concurrent order fills).
func RunBalanceTransaction(ctx context.Context, userEmail string, mutatorFn func(*models.Balance) error) error {
	iter := Client.Collection("balances").
		Where("email", "==", userEmail).
		Limit(1).
		Documents(ctx)

	doc, err := iter.Next()
	if err == iterator.Done {
		return fmt.Errorf("balance not found for %s", userEmail)
	}
	if err != nil {
		return fmt.Errorf("failed to query balance for %s: %w", userEmail, err)
	}

	docRef := doc.Ref

	return Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snap, err := tx.Get(docRef)
		if err != nil {
			return fmt.Errorf("failed to get balance in transaction: %w", err)
		}

		var balance models.Balance
		if err := snap.DataTo(&balance); err != nil {
			return fmt.Errorf("failed to deserialize balance: %w", err)
		}

		if err := mutatorFn(&balance); err != nil {
			return err
		}

		return tx.Set(docRef, balance)
	})
}