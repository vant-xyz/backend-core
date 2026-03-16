package handlers

import (
	"crypto/rand"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/vant-xyz/backend-code/models"
	"github.com/vant-xyz/backend-code/services"
)

func GenerateReferralCode() string {
	b := make([]byte, 3)
	rand.Read(b)
	return fmt.Sprintf("%X", b)
}

func JoinWaitlist(c *gin.Context) {
	var req models.WaitlistRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.WaitlistResponse{
			Success: false,
			Message: "Invalid request payload",
		})
		return
	}

	var existingEmail string
	err := services.DB.QueryRow("SELECT email FROM waitlist WHERE email = $1", req.Email).Scan(&existingEmail)
	if err == nil {
		c.JSON(http.StatusOK, models.WaitlistResponse{
			Success: true,
			Message: "You are already on the waitlist!",
		})
		return
	}

	referralCode := GenerateReferralCode()

	_, err = services.DB.Exec(
		"INSERT INTO waitlist (email, referral_code, referred_by) VALUES ($1, $2, $3)",
		req.Email, referralCode, req.ReferralCode,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.WaitlistResponse{
			Success: false,
			Message: "Failed to join waitlist",
		})
		return
	}

	go services.SendWaitlistEmail(req.Email, referralCode)

	c.JSON(http.StatusOK, models.WaitlistResponse{
		Success: true,
		Message: "Successfully joined the waitlist!",
	})
}
