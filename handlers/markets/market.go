package markets

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/vant-xyz/backend-code/models"
	marketsvc "github.com/vant-xyz/backend-code/services/markets"
)

func CreateMarketGEM(c *gin.Context) {
	var req struct {
		Title           string `json:"title" binding:"required"`
		Description     string `json:"description" binding:"required"`
		DataProvider    string `json:"data_provider" binding:"required"`
		StartTimeUTC    int64  `json:"start_time_utc" binding:"required"`
		DurationSeconds uint64 `json:"duration_seconds" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request: " + err.Error()})
		return
	}

	market, err := marketsvc.CreateGEM(c.Request.Context(), marketsvc.CreateGEMInput{
		Title:           req.Title,
		Description:     req.Description,
		DataProvider:    req.DataProvider,
		StartTimeUTC:    time.Unix(req.StartTimeUTC, 0).UTC(),
		DurationSeconds: req.DurationSeconds,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to create GEM market: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"market":  market,
	})
}

func SettleMarketGEM(c *gin.Context) {
	marketID := c.Param("id")
	if marketID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Market ID required"})
		return
	}

	var req struct {
		Outcome            string `json:"outcome" binding:"required"`
		OutcomeDescription string `json:"outcome_description" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request: " + err.Error()})
		return
	}

	outcome := models.MarketOutcome(req.Outcome)
	if outcome != models.OutcomeYes && outcome != models.OutcomeNo {
		c.JSON(http.StatusBadRequest, gin.H{"message": "outcome must be YES or NO"})
		return
	}

	if err := marketsvc.SettleGEM(c.Request.Context(), marketsvc.SettleGEMInput{
		MarketID:           marketID,
		Outcome:            outcome,
		OutcomeDescription: req.OutcomeDescription,
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to settle GEM market: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"market_id": marketID,
		"outcome":   req.Outcome,
	})
}

func GetMarket(c *gin.Context) {
	marketID := c.Param("id")
	if marketID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Market ID required"})
		return
	}

	market, err := marketsvc.GetMarketByID(c.Request.Context(), marketID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Market not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"market":  market,
	})
}

func GetMarkets(c *gin.Context) {
	marketType := c.Query("type")
	status := c.Query("status")
	asset := c.Query("asset")
	limitStr := c.DefaultQuery("limit", "50")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > 200 {
		limit = 50
	}

	var markets []models.Market

	switch {
	case marketType != "" && status == "active":
		markets, err = marketsvc.GetActiveMarketsByType(c.Request.Context(), models.MarketType(marketType))
	case marketType != "":
		markets, err = marketsvc.GetMarketsByType(c.Request.Context(), models.MarketType(marketType))
	case asset != "":
		markets, err = marketsvc.GetMarketsByAsset(c.Request.Context(), asset)
	case status == "active":
		markets, err = marketsvc.GetActiveMarkets(c.Request.Context())
	case status == "resolved":
		markets, err = marketsvc.GetResolvedMarkets(c.Request.Context())
	default:
		markets, err = marketsvc.GetActiveMarkets(c.Request.Context())
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to fetch markets: " + err.Error()})
		return
	}

	if markets == nil {
		markets = []models.Market{}
	}

	if len(markets) > limit {
		markets = markets[:limit]
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"markets": markets,
		"count":   len(markets),
	})
}

func SyncMarket(c *gin.Context) {
	marketID := c.Param("id")
	if marketID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Market ID required"})
		return
	}

	market, err := marketsvc.SyncMarketFromChain(c.Request.Context(), marketID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to sync market: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"market":  market,
	})
}