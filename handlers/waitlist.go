package handlers

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/vant-xyz/backend-code/models"
	"github.com/vant-xyz/backend-code/services"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

	ctx := context.Background()
	docRef := services.FirestoreClient.Collection("waitlist").Doc(req.Email)
	_, err := docRef.Get(ctx)
	if err == nil {
		c.JSON(http.StatusOK, models.WaitlistResponse{
			Success: true,
			Message: "You are already on the waitlist!",
		})
		return
	}

	if status.Code(err) != codes.NotFound {
		c.JSON(http.StatusInternalServerError, models.WaitlistResponse{
			Success: false,
			Message: "Database error",
		})
		return
	}

	referralCode := GenerateReferralCode()

	_, err = docRef.Set(ctx, map[string]interface{}{
		"email":         req.Email,
		"referral_code": referralCode,
		"referred_by":   req.ReferralCode,
		"created_at":    time.Now(),
	})
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
