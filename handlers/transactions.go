package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/vant-xyz/backend-code/db"
)

func GetTransactions(c *gin.Context) {
	email, _ := c.Get("email")

	transactions, err := db.GetTransactionsByEmail(c.Request.Context(), email.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to fetch transactions"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":      true,
		"transactions": transactions,
	})
}
