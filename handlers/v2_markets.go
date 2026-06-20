package handlers

import (
	"net/url"

	"github.com/gin-gonic/gin"
)

// GetMarket proxies GET /v2/markets/:id → Jupiter GET /markets/:id
func GetMarket(c *gin.Context) {
	passthroughGet(c, "/markets/"+c.Param("id"), nil)
}

// GetOrderbook proxies GET /v2/orderbook/:id → Jupiter GET /orderbook/:id
func GetOrderbook(c *gin.Context) {
	passthroughGet(c, "/orderbook/"+c.Param("id"), nil)
}

// GetTradingStatus proxies GET /v2/trading-status → Jupiter GET /trading-status
func GetTradingStatus(c *gin.Context) {
	passthroughGet(c, "/trading-status", nil)
}

// GetEventMarkets proxies GET /v2/events/:id/markets → Jupiter GET /markets?eventId=:id
func GetEventMarkets(c *gin.Context) {
	passthroughGet(c, "/markets", url.Values{"eventId": {c.Param("id")}})
}
