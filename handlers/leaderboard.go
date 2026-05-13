package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/vant-xyz/backend-code/db"
)

func GetLeaderboard(c *gin.Context) {
	isDemo := c.DefaultQuery("demo", "true") == "true"
	limit := 50
	if l, err := strconv.Atoi(c.DefaultQuery("limit", "50")); err == nil && l > 0 && l <= 200 {
		limit = l
	}

	entries, err := db.GetLeaderboard(c.Request.Context(), isDemo, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load leaderboard"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"leaderboard": entries})
}

func GetMyLeaderboardRank(c *gin.Context) {
	email, exists := c.Get("email")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	isDemo := c.DefaultQuery("demo", "true") == "true"

	entry, err := db.GetLeaderboardEntry(c.Request.Context(), email.(string), isDemo)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load rank"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"entry": entry})
}
