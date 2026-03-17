package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/vant-xyz/backend-code/services"
)

func GetPrices(c *gin.Context) {
	prices := services.GetLatestPrices()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"prices":  prices,
	})
}
