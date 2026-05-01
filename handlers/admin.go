package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	solanago "github.com/gagliardetto/solana-go"
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

func AdminPing(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"ok": true})
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

	resp := gin.H{
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
	}

	if market.Status == models.MarketStatusResolved {
		settled, err := db.GetSettledPositionsWithUsers(c.Request.Context(), marketID)
		if err == nil {
			winners := []gin.H{}
			losers := []gin.H{}
			for _, sp := range settled {
				isWinner := (market.Outcome == models.OutcomeYes && sp.Side == models.OrderSideYes) ||
					(market.Outcome == models.OutcomeNo && sp.Side == models.OrderSideNo)
				entry := gin.H{"email": sp.UserEmail, "username": sp.Username, "side": string(sp.Side), "shares": sp.Shares, "payout": sp.Payout}
				if isWinner {
					winners = append(winners, entry)
				} else {
					losers = append(losers, entry)
				}
			}
			resp["winners"] = winners
			resp["losers"] = losers
		}
	}

	c.JSON(http.StatusOK, resp)
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

func SearchAdminMarkets(c *gin.Context) {
	query := c.Query("q")
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "q is required"})
		return
	}
	marketType := c.Query("type")

	markets, err := db.SearchMarkets(c.Request.Context(), query, marketType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Search failed: " + err.Error()})
		return
	}
	if markets == nil {
		markets = []models.Market{}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "markets": markets, "count": len(markets)})
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

func GetCAPPMPrice(c *gin.Context) {
	asset := c.Query("asset")
	atStr := c.Query("at")
	if asset == "" || atStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "asset and at query params required"})
		return
	}

	at, err := time.Parse(time.RFC3339, atStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "at must be RFC3339 e.g. 2026-04-25T15:38:42Z"})
		return
	}

	priceCents, err := marketsvc.GetHistoricalPrice(asset, at)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": "price fetch failed: " + err.Error()})
		return
	}

	dollars := priceCents / 100
	cents := priceCents % 100
	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"asset":       asset,
		"at":          at.UTC().Format(time.RFC3339),
		"price_cents": priceCents,
		"price_usd":   fmt.Sprintf("%d.%02d", dollars, cents),
	})
}

func ForceSettleCAPPM(c *gin.Context) {
	marketID := c.Param("id")
	if marketID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "market ID required"})
		return
	}

	var req struct {
		EndPriceCents uint64 `json:"end_price_cents" binding:"required"`
		SkipOnchain   bool   `json:"skip_onchain"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "end_price_cents (integer) required"})
		return
	}

	var settleErr error
	if req.SkipOnchain {
		settleErr = marketsvc.SettleCAPPMOffChain(c.Request.Context(), marketID, req.EndPriceCents)
	} else {
		settleErr = marketsvc.SettleCAPPM(c.Request.Context(), marketID, req.EndPriceCents)
	}
	if settleErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "settlement failed: " + settleErr.Error()})
		return
	}

	if !req.SkipOnchain {
		go func() {
			market, err := marketsvc.GetMarketByID(context.Background(), marketID)
			if err != nil {
				log.Printf("[Admin] cappm-settle: failed to load market %s for payout dispatch: %v", marketID, err)
				return
			}
			pCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			result, err := marketsvc.ProcessMarketSettlement(pCtx, marketID, market.Outcome)
			if err != nil {
				log.Printf("[Admin] cappm-settle: payout distribution failed for %s: %v", marketID, err)
				return
			}
			log.Printf("[Admin] cappm-settle: payouts distributed: market=%s winners=%d payout=%.2f",
				marketID, result.WinningCount, result.TotalPayout)
		}()
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "market_id": marketID, "end_price_cents": req.EndPriceCents})
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

func GetAdminUsers(c *gin.Context) {
	search := c.Query("q")
	sortBy := c.Query("sort")
	page := 1
	pageSize := 20
	if p := c.Query("page"); p != "" {
		fmt.Sscanf(p, "%d", &page)
		if page < 1 {
			page = 1
		}
	}
	offset := (page - 1) * pageSize

	users, total, err := db.GetAdminUsers(c.Request.Context(), search, sortBy, pageSize, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to fetch users: " + err.Error()})
		return
	}
	if users == nil {
		users = []db.UserAdminStats{}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"users":   users,
		"total":   total,
		"page":    page,
		"pages":   (total + int64(pageSize) - 1) / int64(pageSize),
	})
}

func GetAdminUser(c *gin.Context) {
	email := c.Param("email")
	if email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "email required"})
		return
	}
	u, err := db.GetAdminUserByEmail(c.Request.Context(), email)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "user": u})
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

func DumpFeeWalletToUSDC(c *gin.Context) {
	raw := os.Getenv("VANTIC_FEE_WALLET_SOL_PRIVATE_KEY")
	if raw == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "VANTIC_FEE_WALLET_SOL_PRIVATE_KEY not set"})
		return
	}

	var keyBytes []byte
	if err := json.Unmarshal([]byte(raw), &keyBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to parse fee wallet key: " + err.Error()})
		return
	}
	privKey := solanago.PrivateKey(keyBytes)

	results, err := services.DumpWalletToUSDC(c.Request.Context(), privKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "wallet": privKey.PublicKey().String(), "swaps": results})
}
