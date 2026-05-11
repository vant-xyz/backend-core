package markets

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/vant-xyz/backend-code/models"
	marketsvc "github.com/vant-xyz/backend-code/services/markets"
)

type orderRequest struct {
	MarketID  string  `json:"market_id"`
	Side      string  `json:"side" binding:"required"`
	Type      string  `json:"type" binding:"required"`
	Price     float64 `json:"price"`
	Quantity  float64 `json:"quantity" binding:"required"`
	IsDemo    bool    `json:"is_demo"`
	ExpiresAt *int64  `json:"expires_at"`
}

func PlaceOrder(c *gin.Context) {
	email, _ := c.Get("email")
	userEmail := email.(string)

	var req orderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request: " + err.Error()})
		return
	}

	order, err := submitOrder(c, userEmail, req.MarketID, req)
	if err != nil {
		writeNormalizedMarketError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"order":   order,
	})
}

func BuyOrder(c *gin.Context) {
	email, _ := c.Get("email")
	userEmail := email.(string)

	marketID := c.Param("id")
	if marketID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Market ID required"})
		return
	}

	var req orderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request: " + err.Error()})
		return
	}

	order, err := submitOrder(c, userEmail, marketID, req)
	if err != nil {
		writeNormalizedMarketError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"order":   order,
	})
}

func SellOrder(c *gin.Context) {
	email, _ := c.Get("email")
	userEmail := email.(string)

	marketID := c.Param("id")
	if marketID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Market ID required"})
		return
	}

	var req struct {
		PositionID string  `json:"position_id" binding:"required"`
		Shares     float64 `json:"shares"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request: " + err.Error()})
		return
	}

	position, proceeds, err := marketsvc.ClosePosition(c.Request.Context(), marketsvc.ClosePositionInput{
		PositionID: req.PositionID,
		MarketID:   marketID,
		UserEmail:  userEmail,
		Shares:     req.Shares,
	})
	if err != nil {
		writeNormalizedMarketError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"position": position,
		"proceeds": proceeds,
	})
}

func submitOrder(c *gin.Context, userEmail, marketID string, req orderRequest) (*models.Order, error) {
	side := models.OrderSide(req.Side)
	if side != models.OrderSideYes && side != models.OrderSideNo {
		return nil, fmt.Errorf("side must be YES or NO")
	}

	orderType := models.OrderType(req.Type)
	if orderType != models.OrderTypeLimit && orderType != models.OrderTypeMarket {
		return nil, fmt.Errorf("type must be LIMIT or MARKET")
	}

	if req.MarketID != "" && marketID != "" && req.MarketID != marketID {
		return nil, fmt.Errorf("market_id must match path id")
	}

	if marketID == "" {
		marketID = req.MarketID
	}
	if marketID == "" {
		return nil, fmt.Errorf("market ID required")
	}

	if orderType == models.OrderTypeLimit && req.Price <= 0 {
		return nil, fmt.Errorf("price is required for limit orders")
	}

	if req.Quantity <= 0 {
		return nil, fmt.Errorf("quantity must be positive")
	}

	var expiresAt *time.Time
	if req.ExpiresAt != nil {
		t := time.Unix(*req.ExpiresAt, 0).UTC()
		expiresAt = &t
	}

	return marketsvc.PlaceOrder(c.Request.Context(), marketsvc.PlaceOrderInput{
		UserEmail: userEmail,
		MarketID:  marketID,
		Side:      side,
		Type:      orderType,
		Price:     req.Price,
		Quantity:  req.Quantity,
		IsDemo:    req.IsDemo,
		ExpiresAt: expiresAt,
	})
}

func CancelOrder(c *gin.Context) {
	email, _ := c.Get("email")
	userEmail := email.(string)

	orderID := c.Param("id")
	if orderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Order ID required"})
		return
	}

	if err := marketsvc.CancelOrder(c.Request.Context(), orderID, userEmail); err != nil {
		writeNormalizedMarketError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"order_id": orderID,
	})
}

func GetOrderbook(c *gin.Context) {
	marketID := c.Param("id")
	if marketID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Market ID required"})
		return
	}

	snapshot, err := marketsvc.GetOrderbook(c.Request.Context(), marketID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to fetch orderbook: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"orderbook": snapshot,
	})
}

func GetOrderbookDepth(c *gin.Context) {
	marketID := c.Param("id")
	if marketID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Market ID required"})
		return
	}

	levelsStr := c.DefaultQuery("levels", "10")
	levels, err := strconv.Atoi(levelsStr)
	if err != nil || levels <= 0 || levels > 50 {
		levels = 10
	}

	sideStr := c.DefaultQuery("side", "YES")
	side := models.OrderSide(sideStr)
	if side != models.OrderSideYes && side != models.OrderSideNo {
		c.JSON(http.StatusBadRequest, gin.H{"message": "side must be YES or NO"})
		return
	}

	engine := marketsvc.GetMatchingEngine()
	bids := engine.GetDepth(marketID, side, "bids")
	asks := engine.GetDepth(marketID, side, "asks")

	if len(bids) > levels {
		bids = bids[:levels]
	}
	if len(asks) > levels {
		asks = asks[:levels]
	}

	spread := 0.0
	if len(bids) > 0 && len(asks) > 0 {
		spread = asks[0].Price - bids[0].Price
	}

	c.JSON(http.StatusOK, gin.H{
		"success":           true,
		"market_id":         marketID,
		"side":              side,
		"bids":              bids,
		"asks":              asks,
		"spread":            spread,
		"last_traded_price": engine.GetLastTradedPrice(marketID),
	})
}

func GetUserOrders(c *gin.Context) {
	email, _ := c.Get("email")
	userEmail := email.(string)

	marketID := c.Query("market_id")

	orders, err := marketsvc.GetUserOrders(c.Request.Context(), userEmail, marketID)
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

func GetUserPositions(c *gin.Context) {
	email, _ := c.Get("email")
	userEmail := email.(string)

	marketID := c.Query("market_id")

	positions, err := marketsvc.GetUserPositions(c.Request.Context(), userEmail, marketID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to fetch positions: " + err.Error()})
		return
	}

	if positions == nil {
		positions = []models.Position{}
	}

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"positions": positions,
		"count":     len(positions),
	})
}

func HandleOrderbookWS(c *gin.Context) {
	marketsvc.HandleOrderbookWS(c)
}
