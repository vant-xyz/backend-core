package models

import "time"

type Transaction struct {
	ID        string    `json:"id" firestore:"id"`
	UserEmail string    `json:"user_email" firestore:"user_email"`
	Amount    float64   `json:"amount" firestore:"amount"`
	Currency  string    `json:"currency" firestore:"currency"`
	Nature    string    `json:"nature" firestore:"nature"` // "demo" or "real"
	Type      string    `json:"type" firestore:"type"`     // "deposit", "withdrawal", "wager", "faucet"
	Status    string    `json:"status" firestore:"status"` // "pending", "completed", "failed"
	TxHash    string    `json:"tx_hash,omitempty" firestore:"tx_hash"`
	CreatedAt time.Time `json:"created_at" firestore:"created_at"`
}
