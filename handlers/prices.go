package handlers

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/vant-xyz/backend-code/services"
)

type tokenPricesCacheEntry struct {
	key       string
	expiresAt time.Time
	prices    map[string]float64
}

var (
	tokenPricesCacheMu sync.RWMutex
	tokenPricesCache   tokenPricesCacheEntry
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
	cacheKey := strings.Join(tickers, ",")

	tokenPricesCacheMu.RLock()
	if tokenPricesCache.key == cacheKey && time.Now().Before(tokenPricesCache.expiresAt) {
		cached := tokenPricesCache.prices
		tokenPricesCacheMu.RUnlock()
		c.JSON(http.StatusOK, gin.H{"success": true, "prices": cached})
		return
	}
	tokenPricesCacheMu.RUnlock()

	prices, err := services.GetTokenPrices(tickers)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to fetch prices: " + err.Error()})
		return
	}

	tokenPricesCacheMu.Lock()
	tokenPricesCache = tokenPricesCacheEntry{
		key:       cacheKey,
		expiresAt: time.Now().Add(5 * time.Second),
		prices:    prices,
	}
	tokenPricesCacheMu.Unlock()

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
