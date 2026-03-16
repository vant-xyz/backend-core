package handlers

import (
	"context"
	"net/http"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/vant-xyz/backend-code/services"
)

func HealthCheck(c *gin.Context) {
	ctx := context.Background()

	err := services.FirestoreClient.RunTransaction(ctx, func(ctx context.Background(), tx *firestore.Transaction) error {
		return nil
	})

	status := "up"
	if err != nil {
		status = "down"
	}

	c.JSON(http.StatusOK, gin.H{
		"status":   status,
		"database": status,
		"message":  "Vant Backend is healthy",
	})
}
