package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gagliardetto/solana-go"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/handlers"
	handlersmarkets "github.com/vant-xyz/backend-code/handlers/markets"
	"github.com/vant-xyz/backend-code/services"
	marketsvc "github.com/vant-xyz/backend-code/services/markets"
)

// validateV2Env fails fast at boot if any environment variable required for the
// v2 (Jupiter prediction) trading flow is missing or malformed. Without these a
// trade would only fail later, at request time, with a confusing error.
func validateV2Env() {
	required := []string{
		"JUPITER_API_KEY",        // building orders / fetching events
		"MAINNET_SOLANA_RPC_URL", // submitting signed transactions
		"V2_FEE_WALLET",          // destination for the Vantic fee
	}
	for _, k := range required {
		if strings.TrimSpace(os.Getenv(k)) == "" {
			log.Fatalf("%s environment variable is required for v2 trading", k)
		}
	}
	// V2_FEE_WALLET must be a valid pubkey or every buy will fail fee injection.
	if _, err := solana.PublicKeyFromBase58(os.Getenv("V2_FEE_WALLET")); err != nil {
		log.Fatalf("V2_FEE_WALLET is not a valid Solana pubkey: %v", err)
	}
}

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

	services.ValidateJWTSecret()
	validateV2Env()

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
	services.StartJupiterSnapshotPoller()
	marketsvc.StartCAPPMService()
	marketsvc.StartGlobalLiquidityManager()
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

	// ── Docs & Health (root, unversioned) ────────────────────────────────────
	r.GET("/docs", swaggerUIHandler)
	r.GET("/docs/swagger.yaml", swaggerSpecHandler)
	r.GET("/health", handlers.HealthCheck)

	// ── v2 API ────────────────────────────────────────────────────────────────
	v2 := r.Group("/v2")
	{
		// auth (public)
		v2.GET("/auth/nonce", handlers.GetNonce)
		v2.POST("/auth/verify", handlers.VerifyWallet)

		// events (public) — static paths must be registered before :id wildcard
		v2.GET("/events/worldcup", handlers.GetWorldCupEvents)
		v2.GET("/events/search", handlers.SearchEvents)
		v2.GET("/events/scores", handlers.GetEventScores)
		v2.GET("/events", handlers.GetEvents)
		v2.GET("/events/:id", handlers.GetEvent)
		v2.GET("/events/:id/score", handlers.GetEventScore)
		v2.GET("/events/:id/markets", handlers.GetEventMarkets)
		v2.GET("/events/:id/price-history", handlers.GetEventPriceHistory)

		// markets + orderbook (public)
		v2.GET("/markets/:id", handlers.GetMarket)
		v2.GET("/orderbook/:id", handlers.GetOrderbook)
		v2.GET("/trading-status", handlers.GetTradingStatus)

		// transactions (requires signed tx from wallet)
		v2.POST("/transactions/submit", handlers.SubmitTransaction)

		// orders + positions (v2 JWT required)
		v2auth := v2.Group("/")
		v2auth.Use(handlers.AuthMiddleware())
		{
			v2auth.POST("/orders", handlers.CreateOrder)
			v2auth.GET("/positions", handlers.GetPositions)
			v2auth.DELETE("/positions/:positionPubkey", handlers.ClosePosition)
			v2auth.POST("/positions/:positionPubkey/claim", handlers.ClaimPosition)
		}
	}

	// ── v1 API ────────────────────────────────────────────────────────────────
	v1 := r.Group("/v1")
	{
		// ── Public ────────────────────────────────────────────────────────────
		v1.POST("/waitlist", handlers.JoinWaitlist)

		v1.GET("/prices", handlers.GetPrices)
		v1.GET("/prices/vant", handlers.GetVantPrices)
		v1.GET("/prices/vant/:asset", handlers.GetAssetPrice)
		v1.GET("/prices/tokens", handlers.GetJupiterTokenPrices)

		v1.POST("/auth/exists", handlers.CheckEmailExists)
		v1.POST("/auth/username/exists", handlers.CheckUsername)
		v1.POST("/auth", handlers.Auth)
		v1.GET("/auth/google", handlers.GoogleLogin)
		v1.GET("/auth/google/callback", handlers.GoogleCallback)

		v1internal := v1.Group("/internal")
		v1internal.Use(handlers.IndexerKeyMiddleware())
		{
			v1internal.GET("/wallets", handlers.GetInternalWallets)
			v1internal.POST("/deposit", handlers.HandleInternalDeposit)
		}

		v1.GET("/leaderboard", handlers.GetLeaderboard)

		// ── Markets — public ──────────────────────────────────────────────────
		v1.GET("/markets", handlersmarkets.GetMarkets)
		v1.GET("/markets/:id", handlersmarkets.GetMarket)
		v1.GET("/markets/:id/orderbook", handlersmarkets.GetOrderbook)
		v1.GET("/markets/:id/orderbook/depth", handlersmarkets.GetOrderbookDepth)
		v1.GET("/markets/:id/candles", handlersmarkets.GetMarketCandles)
		v1.GET("/markets/:id/opinion-trend", handlersmarkets.GetMarketOpinionTrend)
		v1.GET("/markets/:id/volume", handlersmarkets.GetMarketVolume)
		v1.GET("/markets/:id/stats", handlersmarkets.GetMarketStats)
		v1.GET("/markets/:id/history", handlersmarkets.GetMarketHistory)
		v1.GET("/markets/:id/quote", handlersmarkets.GetMarketFillPreview)
		v1.GET("/markets/:id/trades", handlersmarkets.GetMarketTrades)
		v1.GET("/markets/:id/onchain", handlersmarkets.GetMarketOnchain)
		v1.GET("/markets/onchain", handlersmarkets.GetMarketsOnchain)

		// ── Authenticated ─────────────────────────────────────────────────────
		v1auth := v1.Group("/")
		v1auth.Use(handlers.AuthMiddleware())
		{
			v1auth.GET("/user", handlers.GetUserProfile)
			v1auth.PUT("/user", handlers.UpdateUserProfile)
			v1auth.POST("/user/profile-image", handlers.UploadProfileImage)
			v1auth.POST("/auth/username", handlers.UpdateUsername)
			v1auth.POST("/auth/logout", handlers.Logout)

			v1auth.GET("/balance", handlers.GetUserBalance)
			v1auth.GET("/balance/wsol", handlers.GetUserWSOLBalance)
			v1auth.GET("/balance/sync", handlers.SyncBalance)
			v1auth.POST("/balance/sell", handlers.SellAsset)
			v1auth.POST("/balance/withdraw", handlers.WithdrawBalance)
			v1auth.POST("/balance/withdraw/asset", handlers.WithdrawAsset)
			v1auth.POST("/balance/convert/usdc", handlers.ConvertToUSDC)
			v1auth.GET("/transactions", handlers.GetTransactions)
			v1auth.POST("/transactions/email", handlers.SendTransactionEmail)
			v1auth.POST("/demo/fund", handlers.FundDemoAccount)

			v1auth.POST("/orders", handlersmarkets.PlaceOrder)
			v1auth.POST("/markets/:id/buy", handlersmarkets.BuyOrder)
			v1auth.POST("/markets/:id/sell", handlersmarkets.SellOrder)
			v1auth.POST("/markets/:id/quote", handlersmarkets.ReserveMarketQuote)
			v1auth.POST("/markets/:id/quote/accept", handlersmarkets.AcceptMarketQuote)
			v1auth.DELETE("/orders/:id", handlersmarkets.CancelOrder)
			v1auth.GET("/leaderboard/me", handlers.GetMyLeaderboardRank)
			v1auth.GET("/orders", handlersmarkets.GetUserOrders)
			v1auth.GET("/positions", handlersmarkets.GetUserPositions)
			v1auth.POST("/positions/:id/close", handlersmarkets.ClosePosition)
			v1auth.POST("/vs/events", handlers.CreateVSEvent)
			v1auth.GET("/vs/events", handlers.ListVSEvents)
			v1auth.GET("/vs/events/mine/created", handlers.ListMyCreatedVSEvents)
			v1auth.GET("/vs/events/mine/joined", handlers.ListMyJoinedVSEvents)
			v1auth.GET("/vs/events/:id", handlers.GetVSEvent)
			v1auth.POST("/vs/events/:id/join", handlers.JoinVSEvent)
			v1auth.POST("/vs/events/:id/confirm", handlers.ConfirmVSEvent)
			v1auth.POST("/vs/events/:id/cancel", handlers.CancelVSEvent)

			v1auth.GET("/ws", func(c *gin.Context) {
				email, _ := c.Get("email")
				services.HandlePriceWS(c.Writer, c.Request, email.(string))
			})
			v1auth.GET("/ws/markets/:id/orderbook", handlersmarkets.HandleOrderbookWS)
		}

		// ── Admin ─────────────────────────────────────────────────────────────
		v1admin := v1.Group("/admin")
		v1admin.Use(handlers.AdminAuthMiddleware())
		{
			v1admin.GET("/ping", handlers.AdminPing)
			v1admin.POST("/upload", handlers.AdminUploadImage)
			v1admin.POST("/markets/gem", handlersmarkets.CreateMarketGEM)
			v1admin.POST("/markets/cappm", handlersmarkets.CreateMarketCAPPMAdmin)
			v1admin.POST("/markets/:id/settle", handlersmarkets.SettleMarketGEM)
			v1admin.POST("/markets/:id/sync", handlersmarkets.SyncMarket)
			v1admin.POST("/markets/:id/force-settle", handlers.ForceSettleMarket)
			v1admin.GET("/markets", handlers.GetAllMarkets)
			v1admin.GET("/markets/search", handlers.SearchAdminMarkets)
			v1admin.GET("/markets/:id/stats", handlers.GetMarketStats)
			v1admin.GET("/orders", handlers.GetAllOrders)
			v1admin.GET("/users/:email/exposure", handlers.GetUserExposure)
			v1admin.GET("/cappm/status", handlers.GetCAPPMStatus)
			v1admin.GET("/cappm/price", handlers.GetCAPPMPrice)
			v1admin.POST("/markets/:id/cappm-settle", handlers.ForceSettleCAPPM)
			v1admin.GET("/overview", handlers.GetOverview)
			v1admin.GET("/users", handlers.GetAdminUsers)
			v1admin.GET("/users/:email", handlers.GetAdminUser)
			v1admin.POST("/fee-wallet/dump-usdc", handlers.DumpFeeWalletToUSDC)
			v1admin.POST("/untether-reserve/:wallet_type", handlers.UntetherReserve)
			v1admin.GET("/reserve-wallets/balances", handlers.GetReserveWalletBalances)
			v1admin.POST("/bots/fund", handlers.FundBotAccounts)
		}
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	if err := r.Run(":" + port); err != nil {
		log.Fatal("Failed to run server: ", err)
	}
}
