package main

import (
	"log"
	"os"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/handlers"
)

func main() {
	_ = godotenv.Load()

	db.Init("vant-a2479", "serviceAccount.json")

	r := gin.Default()

	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"POST", "OPTIONS", "GET", "PUT"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

	// Public routes
	r.POST("/waitlist", handlers.JoinWaitlist)
	r.GET("/health", handlers.HealthCheck)
	
	// Auth routes
	r.POST("/auth/exists", handlers.CheckEmailExists)
	r.POST("/auth", handlers.Auth) // Unified Login/Signup

	// Protected routes
	auth := r.Group("/")
	auth.Use(handlers.AuthMiddleware())
	{
		auth.POST("/auth/username", handlers.UpdateUsername)
		auth.POST("/logout", handlers.Logout)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	if err := r.Run(":" + port); err != nil {
		log.Fatal("Failed to run server: ", err)
	}
}
