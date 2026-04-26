package handlers

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/models"
	"github.com/vant-xyz/backend-code/services"
	marketsvc "github.com/vant-xyz/backend-code/services/markets"
)

func AdminAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.GetHeader("X-Admin-Key")
		if key == "" || key != os.Getenv("ADMIN_API_KEY") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "Unauthorized"})
			return
		}
		c.Next()
	}
}

func ForceSettleMarket(c *gin.Context) {
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

	market, err := marketsvc.GetMarketByID(c.Request.Context(), marketID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Market not found"})
		return
	}

	if market.Status == models.MarketStatusResolved {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Market is already resolved"})
		return
	}

	if market.MarketType == models.MarketTypeGEM {
		if err := marketsvc.SettleGEM(c.Request.Context(), marketsvc.SettleGEMInput{
			MarketID:           marketID,
			Outcome:            outcome,
			OutcomeDescription: req.OutcomeDescription,
		}); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to settle GEM market: " + err.Error()})
			return
		}
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"message": "CAPPM markets are auto-settled — use the outcome from Coinbase price data"})
		return
	}

	result, err := marketsvc.ProcessMarketSettlement(c.Request.Context(), marketID, outcome)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Settlement processing failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"result":  result,
	})
}

func GetMarketStats(c *gin.Context) {
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

	orders, err := marketsvc.GetOpenOrdersForMarket(c.Request.Context(), marketID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to fetch orders: " + err.Error()})
		return
	}

	positions, err := marketsvc.GetMarketPositions(c.Request.Context(), marketID, models.PositionStatusActive)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to fetch positions: " + err.Error()})
		return
	}

	var totalVolume, yesVolume, noVolume float64
	var openOrderCount int
	for _, o := range orders {
		openOrderCount++
		value := o.Price * o.RemainingQty
		totalVolume += value
		if o.Side == models.OrderSideYes {
			yesVolume += value
		} else {
			noVolume += value
		}
	}

	var totalShares, yesShares, noShares float64
	uniqueTraders := make(map[string]bool)
	for _, p := range positions {
		totalShares += p.Shares
		uniqueTraders[p.UserEmail] = true
		if p.Side == models.OrderSideYes {
			yesShares += p.Shares
		} else {
			noShares += p.Shares
		}
	}

	engine := marketsvc.GetMatchingEngine()
	lastPrice := engine.GetLastTradedPrice(marketID)

	impliedYesPct := 0.0
	if lastPrice > 0 {
		impliedYesPct = lastPrice
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"market":  market,
		"stats": gin.H{
			"open_order_count":  openOrderCount,
			"open_interest":     totalVolume,
			"yes_open_interest": yesVolume,
			"no_open_interest":  noVolume,
			"total_shares":      totalShares,
			"yes_shares":        yesShares,
			"no_shares":         noShares,
			"unique_traders":    len(uniqueTraders),
			"last_traded_price": lastPrice,
			"implied_yes_pct":   impliedYesPct,
			"quote_currency":    market.QuoteCurrency,
		},
	})
}

func GetUserExposure(c *gin.Context) {
	userEmail := c.Param("email")
	if userEmail == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "User email required"})
		return
	}

	positions, err := marketsvc.GetUserPositions(c.Request.Context(), userEmail, "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to fetch positions: " + err.Error()})
		return
	}

	orders, err := marketsvc.GetUserOrders(c.Request.Context(), userEmail, "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to fetch orders: " + err.Error()})
		return
	}

	type marketExposure struct {
		MarketID      string  `json:"market_id"`
		YesShares     float64 `json:"yes_shares"`
		NoShares      float64 `json:"no_shares"`
		AvgEntryYes   float64 `json:"avg_entry_yes"`
		AvgEntryNo    float64 `json:"avg_entry_no"`
		OpenOrdersVal float64 `json:"open_orders_value"`
		QuoteCurrency string  `json:"quote_currency"`
	}

	exposureMap := make(map[string]*marketExposure)

	for _, p := range positions {
		if p.Status != models.PositionStatusActive {
			continue
		}
		if _, ok := exposureMap[p.MarketID]; !ok {
			exposureMap[p.MarketID] = &marketExposure{
				MarketID:      p.MarketID,
				QuoteCurrency: p.QuoteCurrency,
			}
		}
		exp := exposureMap[p.MarketID]
		if p.Side == models.OrderSideYes {
			exp.YesShares = p.Shares
			exp.AvgEntryYes = p.AvgEntryPrice
		} else {
			exp.NoShares = p.Shares
			exp.AvgEntryNo = p.AvgEntryPrice
		}
	}

	var totalLockedInOrders float64
	for _, o := range orders {
		if o.Status != models.OrderStatusOpen && o.Status != models.OrderStatusPartiallyFilled {
			continue
		}
		val := o.Price * o.RemainingQty
		totalLockedInOrders += val
		if _, ok := exposureMap[o.MarketID]; !ok {
			exposureMap[o.MarketID] = &marketExposure{
				MarketID:      o.MarketID,
				QuoteCurrency: o.QuoteCurrency,
			}
		}
		exposureMap[o.MarketID].OpenOrdersVal += val
	}

	exposureList := make([]*marketExposure, 0, len(exposureMap))
	for _, exp := range exposureMap {
		exposureList = append(exposureList, exp)
	}

	c.JSON(http.StatusOK, gin.H{
		"success":               true,
		"user_email":            userEmail,
		"active_markets":        len(exposureMap),
		"total_locked_in_orders": totalLockedInOrders,
		"exposure":              exposureList,
	})
}

func GetAllMarkets(c *gin.Context) {
	status := c.DefaultQuery("status", "active")
	marketType := c.Query("type")

	var (
		markets []models.Market
		err     error
	)

	switch {
	case marketType != "" && status == "active":
		markets, err = marketsvc.GetActiveMarketsByType(c.Request.Context(), models.MarketType(marketType))
	case marketType != "":
		markets, err = marketsvc.GetMarketsByType(c.Request.Context(), models.MarketType(marketType))
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

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"markets": markets,
		"count":   len(markets),
	})
}

func GetCAPPMStatus(c *gin.Context) {
	enabled := os.Getenv("ENABLE_AUTO_CURATED_CAPPMS") == "true"
	mode := "SETTLEMENT_ONLY"
	if enabled {
		mode = "AUTO_CREATION"
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"enabled": enabled,
		"mode":    mode,
	})
}

func GetOverview(c *gin.Context) {
	ctx := context.Background()

	var (
		userCount         int64
		marketCount       int64
		activeMarkets     int64
		orderCount        int64
		txCount           int64
		tvlReal           float64
		tvlDemo           float64
		totalLocked       float64
	)

	db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&userCount)
	db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM markets`).Scan(&marketCount)
	db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM markets WHERE status = 'active'`).Scan(&activeMarkets)
	db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM orders`).Scan(&orderCount)
	db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM transactions`).Scan(&txCount)
	db.Pool.QueryRow(ctx, `SELECT COALESCE(SUM(naira), 0) FROM balances`).Scan(&tvlReal)
	db.Pool.QueryRow(ctx, `SELECT COALESCE(SUM(demo_naira), 0) FROM balances`).Scan(&tvlDemo)
	db.Pool.QueryRow(ctx, `SELECT COALESCE(SUM(locked_balance), 0) FROM balances`).Scan(&totalLocked)

	c.JSON(http.StatusOK, gin.H{
		"success":        true,
		"users":          userCount,
		"markets":        marketCount,
		"active_markets": activeMarkets,
		"orders":         orderCount,
		"transactions":   txCount,
		"tvl_real":       tvlReal,
		"tvl_demo":       tvlDemo,
		"total_locked":   totalLocked,
	})
}

func GetAllOrders(c *gin.Context) {
	marketID := c.Query("market_id")
	if marketID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "market_id query param required"})
		return
	}

	statusStr := c.Query("status")

	var (
		orders []models.Order
		err    error
	)

	if statusStr != "" {
		orders, err = marketsvc.GetMarketOrders(c.Request.Context(), marketID, models.OrderStatus(statusStr))
	} else {
		orders, err = marketsvc.GetOpenOrdersForMarket(c.Request.Context(), marketID)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to fetch orders: " + err.Error()})
		return
	}

	if orders == nil {
		orders = []models.Order{}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"orders":  orders,
		"count":   len(orders),
	})
}

func AdminUploadImage(c *gin.Context) {
	folder := c.DefaultPostForm("folder", "vant_markets")

	file, header, err := c.Request.FormFile("image")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "image file required"})
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	publicID := fmt.Sprintf("%d%s", time.Now().UnixMilli(), ext)

	url, err := services.UploadImage(c.Request.Context(), file, folder, publicID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "upload failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "url": url})
}