package models

import "time"

type User struct {
	Email           string    `json:"email" firestore:"email"`
	Name            string    `json:"name,omitempty" firestore:"name"`
	Username        string    `json:"username" firestore:"username"`
	Password        string    `json:"-" firestore:"password"` // Hashed
	VantID          string    `json:"vant_id" firestore:"vant_id"`
	BalanceID       string    `json:"balance_id" firestore:"balance_id"`
	Socials         []string  `json:"socials" firestore:"socials"`
	ProfileImageURL string    `json:"profile_image_url" firestore:"profile_image_url"`
	CreatedAt       time.Time `json:"created_at" firestore:"created_at"`
}

type AuthRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type UsernameRequest struct {
	Username string `json:"username" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
}

type AuthResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Token   string `json:"token,omitempty"`
	User    *User  `json:"user,omitempty"`
}
