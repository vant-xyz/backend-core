package markets

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/vant-xyz/backend-code/models"
	marketsvc "github.com/vant-xyz/backend-code/services/markets"
)

func GetMarketCandles(c *gin.Context) {
	marketID := c.Param("id")
	if marketID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Market ID required"})
		return
	}

	candles, intervalSecs, err := marketsvc.GetMarketCandles(c.Request.Context(), marketID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to fetch candles: " + err.Error()})
		return
	}

	if candles == nil {
		candles = []marketsvc.CandlePoint{}
	}

	c.JSON(http.StatusOK, gin.H{
		"success":          true,
		"market_id":        marketID,
		"interval_seconds": intervalSecs,
		"candles":          candles,
		"count":            len(candles),
	})
}

func GetMarketOpinionTrend(c *gin.Context) {
	marketID := c.Param("id")
	if marketID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Market ID required"})
		return
	}

	points, err := marketsvc.GetOpinionTrend(c.Request.Context(), marketID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to fetch opinion trend: " + err.Error()})
		return
	}

	if points == nil {
		points = []marketsvc.TrendPoint{}
	}

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"market_id": marketID,
		"trend":     points,
		"count":     len(points),
	})
}

func GetMarketFillPreview(c *gin.Context) {
	marketID := c.Param("id")
	if marketID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Market ID required"})
		return
	}

	sideStr := c.Query("side")
	side := models.OrderSide(sideStr)
	if side != models.OrderSideYes && side != models.OrderSideNo {
		c.JSON(http.StatusBadRequest, gin.H{"message": "side must be YES or NO"})
		return
	}

	stakeStr := c.Query("stake")
	stake, err := strconv.ParseFloat(stakeStr, 64)
	if err != nil || stake <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "stake must be a positive number"})
		return
	}

	preview, err := marketsvc.GetFillPreview(c.Request.Context(), marketID, side, stake)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to compute fill preview: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"preview": preview,
	})
}

func ReserveMarketQuote(c *gin.Context) {
	email, _ := c.Get("email")
	userEmail := email.(string)

	marketID := c.Param("id")
	if marketID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Market ID required"})
		return
	}

	var req struct {
		Side  string  `json:"side" binding:"required"`
		Stake float64 `json:"stake" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request: " + err.Error()})
		return
	}

	side := models.OrderSide(req.Side)
	if side != models.OrderSideYes && side != models.OrderSideNo {
		c.JSON(http.StatusBadRequest, gin.H{"message": "side must be YES or NO"})
		return
	}
	if req.Stake <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "stake must be a positive number"})
		return
	}

	quote, err := marketsvc.CreateExecutableQuote(c.Request.Context(), marketID, userEmail, side, req.Stake)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"quote":   quote,
	})
}

func AcceptMarketQuote(c *gin.Context) {
	email, _ := c.Get("email")
	userEmail := email.(string)

	marketID := c.Param("id")
	if marketID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Market ID required"})
		return
	}

	var req struct {
		QuoteID string `json:"quote_id" binding:"required"`
		IsDemo  bool   `json:"is_demo"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request: " + err.Error()})
		return
	}

	order, quote, err := marketsvc.AcceptExecutableQuote(c.Request.Context(), marketsvc.AcceptQuoteInput{
		QuoteID:   req.QuoteID,
		UserEmail: userEmail,
		MarketID:  marketID,
		IsDemo:    req.IsDemo,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"quote":   quote,
		"order":   order,
	})
}

type tradeEntry struct {
	ID       string           `json:"id"`
	Side     models.OrderSide `json:"side"`
	Price    float64          `json:"price"`
	Quantity float64          `json:"quantity"`
	FilledAt time.Time        `json:"filled_at"`
}

func GetMarketTrades(c *gin.Context) {
	marketID := c.Param("id")
	if marketID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Market ID required"})
		return
	}

	limitStr := c.DefaultQuery("limit", "50")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > 200 {
		limit = 50
	}

	orders, err := marketsvc.GetMarketTrades(c.Request.Context(), marketID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to fetch trades: " + err.Error()})
		return
	}

	trades := make([]tradeEntry, len(orders))
	for i, o := range orders {
		trades[i] = tradeEntry{
			ID:       o.ID,
			Side:     o.Side,
			Price:    o.Price,
			Quantity: o.FilledQty,
			FilledAt: o.UpdatedAt,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"market_id": marketID,
		"trades":    trades,
		"count":     len(trades),
	})
}
