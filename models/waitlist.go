package models

import "time"

type WaitlistEntry struct {
	ID            int       `json:"id" firestore:"-"`
	Email         string    `json:"email" firestore:"email"`
	ReferralCode  string    `json:"referral_code" firestore:"referral_code"`
	ReferredBy    string    `json:"referred_by,omitempty" firestore:"referred_by"`
	ReferralCount int       `json:"referral_count" firestore:"referral_count"`
	CreatedAt     time.Time `json:"created_at" firestore:"created_at"`
}

type WaitlistRequest struct {
	Email        string `json:"email" binding:"required,email"`
	ReferralCode string `json:"referralCode"`
}

type WaitlistResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}
