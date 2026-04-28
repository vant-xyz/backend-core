package models

import "time"

type Transaction struct {
	ID        string    `json:"id" firestore:"id"`
	UserEmail string    `json:"user_email" firestore:"user_email"`
	Amount    float64   `json:"amount" firestore:"amount"`
	FeeAmount float64   `json:"fee_amount" firestore:"fee_amount"`
	FeeRate   float64   `json:"fee_rate" firestore:"fee_rate"`
	FeeChain  string    `json:"fee_chain" firestore:"fee_chain"`
	FeeWallet string    `json:"fee_wallet" firestore:"fee_wallet"`
	Currency  string    `json:"currency" firestore:"currency"`
	Nature    string    `json:"nature" firestore:"nature"`
	Type      string    `json:"type" firestore:"type"`
	Status    string    `json:"status" firestore:"status"`
	TxHash    string    `json:"tx_hash,omitempty" firestore:"tx_hash"`
	CreatedAt time.Time `json:"created_at" firestore:"created_at"`
}
