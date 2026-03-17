package handlers

import (
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/models"
	"github.com/vant-xyz/backend-code/services"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func CheckEmailExists(c *gin.Context) {
	var req struct {
		Email string `json:"email" binding:"required,email"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid email format"})
		return
	}

	_, err := db.GetUserByEmail(c.Request.Context(), req.Email)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			c.JSON(http.StatusOK, gin.H{"exists": false})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Database error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"exists": true})
}

func CheckUsername(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Username is required"})
		return
	}

	exists, err := db.CheckUsernameExists(c.Request.Context(), req.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Database error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"exists": exists})
}

func Auth(c *gin.Context) {
	var req models.AuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request payload"})
		return
	}

	user, err := db.GetUserByEmail(c.Request.Context(), req.Email)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			// SIGNUP
			hashedPassword, err := services.HashPassword(req.Password)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to secure password"})
				return
			}
			user, err = db.CreateUser(c.Request.Context(), req.Email, hashedPassword)
			if err != nil {
				log.Printf("Signup Error: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to create user profile: " + err.Error()})
				return
			}
			token, err := services.GenerateJWT(user.Email)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to generate auth token"})
				return
			}
			c.JSON(http.StatusOK, models.AuthResponse{
				Success: true,
				Message: "Account created successfully",
				Token:   token,
				User:    user,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Database error"})
		return
	}

	// LOGIN
	if !services.CheckPasswordHash(req.Password, user.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Invalid email or password"})
		return
	}

	token, err := services.GenerateJWT(user.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to generate auth token"})
		return
	}

	c.JSON(http.StatusOK, models.AuthResponse{
		Success: true,
		Message: "Login successful",
		Token:   token,
		User:    user,
	})
}

func UpdateUsername(c *gin.Context) {
	var req models.UsernameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request payload"})
		return
	}

	emailFromToken, exists := c.Get("email")
	if !exists || emailFromToken.(string) != req.Email {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Token mismatch with requested email"})
		return
	}

	err := db.UpdateUsername(c.Request.Context(), req.Email, req.Username)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	// Fetch the updated user and return to frontend
	updatedUser, _ := db.GetUserByEmail(c.Request.Context(), req.Email)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Username updated successfully",
		"user":    updatedUser,
	})
}

func Logout(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Successfully logged out"})
}

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"message": "Authorization header is required"})
			c.Abort()
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"message": "Authorization format must be Bearer {token}"})
			c.Abort()
			return
		}

		email, err := services.VerifyJWT(parts[1])
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"message": "Invalid or expired token"})
			c.Abort()
			return
		}

		c.Set("email", email)
		c.Next()
	}
}
