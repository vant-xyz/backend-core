package handlers

import (
	"net/http"
	"strings"

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
	prices := services.GetLatestPrices()
	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"buy_rate": 1.0,
		"prices":   prices,
	})
}

func GetJupiterTokenPrices(c *gin.Context) {
	tickersStr := c.Query("tickers")
	if tickersStr == "" {
		tickersStr = "SOL,USDC,USDT,USDG,ETH"
	}
	tickers := strings.Split(tickersStr, ",")
	for i, t := range tickers {
		tickers[i] = strings.TrimSpace(t)
	}

	prices, err := services.GetTokenPrices(tickers)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to fetch prices: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "prices": prices})
}

func GetAssetPrice(c *gin.Context) {
	asset := c.Param("asset")
	usdPrice := services.GetAssetToUSD(asset, 1.0)

	if usdPrice == 0 {
		c.JSON(http.StatusNotFound, gin.H{"message": "Asset not supported"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"asset":   asset,
		"price":   usdPrice,
	})
}
