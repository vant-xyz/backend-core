package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/handlers"
	handlersmarkets "github.com/vant-xyz/backend-code/handlers/markets"
	"github.com/vant-xyz/backend-code/services"
	marketsvc "github.com/vant-xyz/backend-code/services/markets"
)

func splitByComma(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func main() {
	_ = godotenv.Load()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}
	db.Init(databaseURL)
	defer db.Close()

	if err := db.RunMigrations(context.Background()); err != nil {
		log.Fatalf("[Migrate] Fatal: %v", err)
	}

	db.InitRedis()

	services.StartPricePoller()
	marketsvc.StartCAPPMService()
	marketsvc.GetMatchingEngine()
	marketsvc.GetOrderbookHub()

	r := gin.Default()

	allowedOrigins := os.Getenv("ALLOWED_ORIGINS")
	if allowedOrigins == "" {
		allowedOrigins = "https://vantic.xyz,https://indexer-core.vantic.xyz,https://vas-api.vantic.xyz,http://localhost:8080,http://localhost:3000"
	}
	origins := []string{}
	for _, o := range splitByComma(allowedOrigins) {
		origins = append(origins, o)
	}

	corsConfig := cors.Config{
		AllowOrigins:     origins,
		AllowMethods:     []string{"POST", "OPTIONS", "GET", "PUT", "DELETE"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "X-Admin-Key", "X-API-Key"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}
	r.Use(cors.New(corsConfig))
	r.Use(handlers.APIKeyMiddleware())

	r.NoRoute(func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		for _, o := range origins {
			if o == origin {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Access-Control-Allow-Credentials", "true")
				break
			}
		}
		c.JSON(http.StatusNotFound, gin.H{"message": "not found"})
	})

	// ── Docs ──────────────────────────────────────────────────────────────────
	r.GET("/docs", swaggerUIHandler)
	r.GET("/docs/swagger.yaml", swaggerSpecHandler)

	// ── Public ────────────────────────────────────────────────────────────────
	r.POST("/waitlist", handlers.JoinWaitlist)
	r.GET("/health", handlers.HealthCheck)

	r.GET("/prices", handlers.GetPrices)
	r.GET("/prices/vant", handlers.GetVantPrices)
	r.GET("/prices/vant/:asset", handlers.GetAssetPrice)

	r.POST("/auth/exists", handlers.CheckEmailExists)
	r.POST("/auth/username/exists", handlers.CheckUsername)
	r.POST("/auth", handlers.Auth)

	r.GET("/internal/wallets", handlers.GetInternalWallets)
	r.POST("/internal/deposit", handlers.HandleInternalDeposit)

	// ── Markets — public ──────────────────────────────────────────────────────
	// GET /markets?type=CAPPM&status=active   → crypto tab feed
	// GET /markets?type=GEM&status=active     → general tab feed
	// GET /markets?status=resolved            → history feed
	r.GET("/markets", handlersmarkets.GetMarkets)
	r.GET("/markets/:id", handlersmarkets.GetMarket)
	r.GET("/markets/:id/orderbook", handlersmarkets.GetOrderbook)
	r.GET("/markets/:id/orderbook/depth", handlersmarkets.GetOrderbookDepth)
	r.GET("/markets/:id/candles", handlersmarkets.GetMarketCandles)
	r.GET("/markets/:id/opinion-trend", handlersmarkets.GetMarketOpinionTrend)
	r.GET("/markets/:id/quote", handlersmarkets.GetMarketFillPreview)
	r.GET("/markets/:id/trades", handlersmarkets.GetMarketTrades)

	// OVM — Onchain Verifiable Markets
	// Returns Postgres record + raw Solana account state + explorer URLs
	r.GET("/markets/:id/onchain", handlersmarkets.GetMarketOnchain)
	r.GET("/markets/onchain", handlersmarkets.GetMarketsOnchain)

	// ── Authenticated ─────────────────────────────────────────────────────────
	auth := r.Group("/")
	auth.Use(handlers.AuthMiddleware())
	{
		auth.GET("/user", handlers.GetUserProfile)
		auth.PUT("/user", handlers.UpdateUserProfile)
		auth.POST("/user/profile-image", handlers.UploadProfileImage)
		auth.POST("/auth/username", handlers.UpdateUsername)
		auth.POST("/auth/logout", handlers.Logout)

		auth.GET("/balance", handlers.GetUserBalance)
		auth.GET("/balance/sync", handlers.SyncBalance)
		auth.POST("/balance/sell", handlers.SellAsset)
		auth.POST("/balance/withdraw", handlers.WithdrawBalance)
		auth.GET("/transactions", handlers.GetTransactions)
		auth.POST("/transactions/email", handlers.SendTransactionEmail)
		auth.POST("/demo/fund", handlers.FundDemoAccount)

		// Orders & positions
		auth.POST("/orders", handlersmarkets.PlaceOrder)
		auth.DELETE("/orders/:id", handlersmarkets.CancelOrder)
		auth.GET("/orders", handlersmarkets.GetUserOrders)
		auth.GET("/positions", handlersmarkets.GetUserPositions)

		// WebSockets
		// /ws             → live price feed (BTC, ETH, SOL every 5s)
		//                   also pushes BALANCE_UPDATE with full balance object
		// /ws/markets/:id/orderbook → live orderbook depth + fills for a market
		auth.GET("/ws", func(c *gin.Context) {
			email, _ := c.Get("email")
			services.HandlePriceWS(c.Writer, c.Request, email.(string))
		})
		auth.GET("/ws/markets/:id/orderbook", handlersmarkets.HandleOrderbookWS)
	}

	// ── Admin ─────────────────────────────────────────────────────────────────
	admin := r.Group("/admin")
	admin.Use(handlers.AdminAuthMiddleware())
	{
		admin.POST("/upload", handlers.AdminUploadImage)
		admin.POST("/markets/gem", handlersmarkets.CreateMarketGEM)
		admin.POST("/markets/cappm", handlersmarkets.CreateMarketCAPPMAdmin)
		admin.POST("/markets/:id/settle", handlersmarkets.SettleMarketGEM)
		admin.POST("/markets/:id/sync", handlersmarkets.SyncMarket)
		admin.POST("/markets/:id/force-settle", handlers.ForceSettleMarket)
		admin.GET("/markets", handlers.GetAllMarkets)
		admin.GET("/markets/:id/stats", handlers.GetMarketStats)
		admin.GET("/orders", handlers.GetAllOrders)
		admin.GET("/users/:email/exposure", handlers.GetUserExposure)
		admin.GET("/cappm/status", handlers.GetCAPPMStatus)
		admin.GET("/overview", handlers.GetOverview)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	if err := r.Run(":" + port); err != nil {
		log.Fatal("Failed to run server: ", err)
	}
}