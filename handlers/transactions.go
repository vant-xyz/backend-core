package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/models"
	"github.com/vant-xyz/backend-code/services"
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

func SendTransactionEmail(c *gin.Context) {
	var tx models.Transaction
	if err := c.ShouldBindJSON(&tx); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid transaction payload"})
		return
	}
	
	go services.SendTransactionEmail(tx.UserEmail, tx)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Transaction email queued",
	})
}
