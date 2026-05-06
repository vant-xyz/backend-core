package handlers

import (
	"context"
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

var updateBalanceFn = db.UpdateBalance
var saveTransactionFn = db.SaveTransaction
var sendTransactionEmailFn = services.SendTransactionEmail
var emitTorqueEventByEmailFn = services.EmitTorqueEventByEmail
var sweepDepositFeeOptimisticFn = services.SweepDepositFeeOptimistic
var broadcastBalanceUpdateFn = func(email string) {
	services.PriceHub.BroadcastToUser(email, "BALANCE_UPDATE")
}

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
	chain := chainFromNetwork(req.Network)
	feeRate := feeRateForDeposit(chain)

	baseAsset := normalizeDepositAsset(req.Asset)
	netAmount, feeAmount := applyFee(req.Amount, feeRate)
	if netAmount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Deposit amount too small after fee"})
		return
	}

	nature := "real"
	dbField := baseAsset
	if isDemo {
		nature = "demo"
		if !strings.HasPrefix(baseAsset, "demo_") {
			dbField = "demo_" + baseAsset
		}
	}

	if err := updateBalanceFn(c.Request.Context(), req.Email, dbField, netAmount); err != nil {
		log.Printf("[Deposit] Failed to update field %s for %s: %v", dbField, req.Email, err)
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to update user balance"})
		return
	}

	transaction := models.Transaction{
		ID:        fmt.Sprintf("TX_%s", utils.RandomAlphanumeric(12)),
		UserEmail: req.Email,
		Amount:    netAmount,
		FeeAmount: feeAmount,
		FeeRate:   feeRate,
		FeeChain:  string(chain),
		FeeWallet: feeWalletForChain(chain),
		Currency:  req.Asset,
		Nature:    nature,
		Type:      "deposit",
		Status:    "completed",
		TxHash:    req.TxHash,
		CreatedAt: time.Now(),
	}
	saveTransactionFn(c.Request.Context(), transaction)

	go func(toEmail string, tx models.Transaction) {
		if err := sendTransactionEmailFn(toEmail, tx); err != nil {
			log.Printf("[Email] Failed to send deposit email to %s (txID: %s): %v", toEmail, tx.ID, err)
		}
	}(req.Email, transaction)

	go func(email string, tx models.Transaction, chain string, grossAmount float64) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		err := emitTorqueEventByEmailFn(
			ctx,
			email,
			"vantic_deposit",
			tx.ID,
			map[string]interface{}{
				"transactionId": tx.ID,
				"asset":         tx.Currency,
				"network":       req.Network,
				"chain":         chain,
				"amountGross":   grossAmount,
				"amountNet":     tx.Amount,
				"feeAmount":     tx.FeeAmount,
				"feeRate":       tx.FeeRate,
				"txHash":        tx.TxHash,
				"nature":        tx.Nature,
				"status":        tx.Status,
				"createdAt":     tx.CreatedAt.Format(time.RFC3339),
			},
		)
		if err != nil {
			log.Printf("[Torque] Failed to emit vantic_deposit for %s txID=%s: %v", email, tx.ID, err)
		}
	}(req.Email, transaction, string(chain), req.Amount)

	sweepDepositFeeOptimisticFn(req.Email, baseAsset, req.Network, feeAmount)

	broadcastBalanceUpdateFn(req.Email)

	log.Printf("[Deposit] Fee applied email=%s asset=%s gross=%.8f fee=%.8f net=%.8f fee_wallet=%s",
		req.Email, req.Asset, req.Amount, feeAmount, netAmount, feeWalletForChain(chain))

	c.JSON(http.StatusOK, gin.H{
		"success":      true,
		"message":      "Deposit processed",
		"gross_amount": req.Amount,
		"fee_amount":   feeAmount,
		"net_amount":   netAmount,
		"fee_wallet":   feeWalletForChain(chain),
	})
}

func normalizeDepositAsset(asset string) string {
	switch asset {
	case "wsol_sol", "wsol":
		return "sol"
	default:
		return asset
	}
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
