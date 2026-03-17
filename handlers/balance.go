package handlers

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/models"
	"github.com/vant-xyz/backend-code/services"
	"github.com/vant-xyz/backend-code/utils"
)

func GetUserBalance(c *gin.Context) {
	email, _ := c.Get("email")

	balance, err := db.GetBalanceByEmail(c.Request.Context(), email.(string))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Balance not found"})
		return
	}

	realNaira, demoNaira := services.ResolveNairaBalances(balance)

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"balance":    balance,
		"naira":      realNaira,
		"demo_naira": demoNaira,
	})
}

func FundDemoAccount(c *gin.Context) {
	email, _ := c.Get("email")

	var req struct {
		AmountNaira float64 `json:"amount" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request"})
		return
	}

	if req.AmountNaira > 20000 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Max request is 20,000 NGN"})
		return
	}

	balance, err := db.GetBalanceByEmail(c.Request.Context(), email.(string))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Balance not found"})
		return
	}

	_, currentDemoNaira := services.ResolveNairaBalances(balance)
	if currentDemoNaira >= 100 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "You must have less than 100 NGN to request funds"})
		return
	}

	wallet, err := db.GetWalletByEmail(c.Request.Context(), email.(string))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Wallet not found"})
		return
	}

	amountSol := services.GetNairaToSol(req.AmountNaira)
	if amountSol == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Conversion error"})
		return
	}

	sig, err := services.FundDemoAccount(wallet.SolPublicKey, amountSol)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Faucet error: " + err.Error()})
		return
	}

	err = db.UpdateBalance(c.Request.Context(), email.(string), "demo_sol", amountSol)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Balance update error"})
		return
	}

	transaction := models.Transaction{
		ID:        fmt.Sprintf("TX_%s", utils.RandomAlphanumeric(12)),
		UserEmail: email.(string),
		Amount:    req.AmountNaira,
		Currency:  "NGN",
		Nature:    "demo",
		Type:      "faucet",
		Status:    "completed",
		TxHash:    sig,
		CreatedAt: time.Now(),
	}
	db.SaveTransaction(c.Request.Context(), transaction)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("Funded %f SOL (~%.2f NGN)", amountSol, req.AmountNaira),
		"tx_hash": sig,
	})
}
