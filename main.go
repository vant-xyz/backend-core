package main

import (
	"log"
	"os"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/handlers"
	"github.com/vant-xyz/backend-code/services"
)

func main() {
	_ = godotenv.Load()

	db.Init("vant-a2479", "serviceAccount.json")
	
	services.StartPricePoller()

	r := gin.Default()

	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"POST", "OPTIONS", "GET", "PUT"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

	r.POST("/waitlist", handlers.JoinWaitlist)
	r.GET("/health", handlers.HealthCheck)
	r.GET("/prices", handlers.GetPrices)
	r.GET("/prices/vant", handlers.GetVantPrices)
	r.GET("/prices/vant/:asset", handlers.GetAssetPrice)
	
	r.POST("/auth/exists", handlers.CheckEmailExists)
	r.POST("/auth/username/exists", handlers.CheckUsername)
	r.POST("/auth", handlers.Auth)

	r.GET("/ws", func(c *gin.Context) {
		services.HandlePriceWS(c.Writer, c.Request)
	})

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
		auth.POST("/demo/fund", handlers.FundDemoAccount)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	if err := r.Run(":" + port); err != nil {
		log.Fatal("Failed to run server: ", err)
	}
}
