package models

type Wallet struct {
	AccountID          string `json:"account_id" firestore:"account_id"`
	Email              string `json:"email" firestore:"email"`
	SolPublicKey       string `json:"sol_public_key" firestore:"sol_public_key"`
	SolPrivateKey      string `json:"-" firestore:"sol_private_key"` // Encrypted
	BasePublicKey      string `json:"base_public_key" firestore:"base_public_key"`
	BasePrivateKey     string `json:"-" firestore:"base_private_key"` // Encrypted
	NairaAccountNumber string `json:"naira_account_number" firestore:"naira_account_number"`
}
