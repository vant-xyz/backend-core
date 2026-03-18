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

func GetVantPrices(c *gin.Context) {
	buyRate := services.GetVantBuyRate()
	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"buy_rate": buyRate,
	})
}

func GetAssetPrice(c *gin.Context) {
	asset := c.Param("asset")
	nairaPrice := services.GetAssetToNaira(asset, 1.0)
	
	if nairaPrice == 0 {
		c.JSON(http.StatusNotFound, gin.H{"message": "Asset not supported"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"asset":   asset,
		"price":   nairaPrice,
	})
}
