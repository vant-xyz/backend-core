package handlers

import (
	"crypto/rand"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/vant-xyz/backend-code/db"
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

	referralCode := GenerateReferralCode()
	alreadyExists, code, err := db.SaveWaitlistEntry(c.Request.Context(), req.Email, referralCode, req.ReferralCode)

	if err != nil {
		c.JSON(http.StatusInternalServerError, models.WaitlistResponse{
			Success: false,
			Message: "Database error",
		})
		return
	}

	if alreadyExists {
		c.JSON(http.StatusOK, models.WaitlistResponse{
			Success: true,
			Message: "You are already on the waitlist!",
		})
		return
	}

	go db.TrackReferral(req.ReferralCode, req.Email)
	go services.SendWaitlistEmail(req.Email, code)

	c.JSON(http.StatusOK, models.WaitlistResponse{
		Success: true,
		Message: "Successfully joined the waitlist!",
	})
}
