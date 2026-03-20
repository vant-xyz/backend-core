package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/models"
	"github.com/vant-xyz/backend-code/services"
	"github.com/vant-xyz/backend-code/utils"
)

func GetInternalWallets(c *gin.Context) {
	wallets, err := db.GetAllWallets(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to fetch wallets"})
		return
	}

	c.JSON(http.StatusOK, wallets)
}

func HandleInternalDeposit(c *gin.Context) {
	var req struct {
		Email   string  `json:"email" binding:"required"`
		Asset   string  `json:"asset" binding:"required"`
		Amount  float64 `json:"amount" binding:"required"`
		TxHash  string  `json:"tx_hash" binding:"required"`
		Network string  `json:"network" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid deposit payload"})
		return
	}

	isDemo := strings.Contains(req.Network, "devnet") || strings.Contains(req.Network, "testnet")

	nature := "real"
	dbField := req.Asset
	if isDemo {
		nature = "demo"
		if !strings.HasPrefix(req.Asset, "demo_") {
			dbField = "demo_" + req.Asset
		}
	}

	if err := db.UpdateBalance(c.Request.Context(), req.Email, dbField, req.Amount); err != nil {
		log.Printf("[Deposit] Failed to update field %s for %s: %v", dbField, req.Email, err)
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to update user balance"})
		return
	}

	transaction := models.Transaction{
		ID:        fmt.Sprintf("TX_%s", utils.RandomAlphanumeric(12)),
		UserEmail: req.Email,
		Amount:    req.Amount,
		Currency:  req.Asset,
		Nature:    nature,
		Type:      "deposit",
		Status:    "completed",
		TxHash:    req.TxHash,
		CreatedAt: time.Now(),
	}
	db.SaveTransaction(c.Request.Context(), transaction)

	go func(toEmail string, tx models.Transaction) {
		if err := services.SendTransactionEmail(toEmail, tx); err != nil {
			log.Printf("[Email] Failed to send deposit email to %s (txID: %s): %v", toEmail, tx.ID, err)
		}
	}(req.Email, transaction)

	services.PriceHub.BroadcastToUser(req.Email, "BALANCE_UPDATE")

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Deposit processed"})
}

func HandleIndexerWhitelist(c *gin.Context) {
	var req struct {
		Email         string `json:"email" binding:"required"`
		SolPublicKey  string `json:"sol_public_key"`
		BasePublicKey string `json:"base_public_key"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid whitelist payload"})
		return
	}

	go func() {
		if err := services.NotifyIndexerWhitelist(req.Email, req.SolPublicKey, req.BasePublicKey); err != nil {
			log.Printf("[Indexer] Failed to notify indexer for %s: %v", req.Email, err)
		} else {
			log.Printf("[Indexer] Whitelist updated for: %s, SOL: %s, BASE: %s", req.Email, req.SolPublicKey, req.BasePublicKey)
		}
	}()

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Whitelist update received and forwarded to indexer"})
}