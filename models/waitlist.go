package models

import "time"

type WaitlistEntry struct {
	ID           int       `json:"id" db:"id"`
	Email        string    `json:"email" db:"email" binding:"required,email"`
	ReferralCode string    `json:"referral_code" db:"referral_code"`
	ReferredBy   string    `json:"referred_by,omitempty" db:"referred_by"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
}

type WaitlistRequest struct {
	Email        string `json:"email" binding:"required,email"`
	ReferralCode string `json:"referralCode"`
}

type WaitlistResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}
