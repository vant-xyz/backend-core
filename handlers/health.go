package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/vant-xyz/backend-code/db"
)

func HealthCheck(c *gin.Context) {
	err := db.HealthCheck(c.Request.Context())

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
