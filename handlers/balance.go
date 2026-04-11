package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
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
	balance.TotalNaira = realNaira
	balance.TotalDemoNaira = demoNaira

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"balance": balance,
	})
}

func SyncBalance(c *gin.Context) {
	email, _ := c.Get("email")

	wallet, err := db.GetWalletByEmail(c.Request.Context(), email.(string))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Wallet not found"})
		return
	}

	log.Printf("[SyncBalance] Starting sync for %s (wallet: %s)", email, wallet.SolPublicKey)

	onChainSol, err := services.GetSolBalance(wallet.SolPublicKey)
	if err != nil {
		log.Printf("[SyncBalance] Failed to fetch SOL balance for %s: %v", wallet.SolPublicKey, err)
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to fetch on-chain SOL balance"})
		return
	}

	log.Printf("[SyncBalance] SOL balance: %f", onChainSol)

	if err = db.SetBalance(c.Request.Context(), email.(string), "demo_sol", onChainSol); err != nil {
		log.Printf("[SyncBalance] Failed to update SOL balance for %s: %v", email, err)
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to update SOL balance"})
		return
	}

	usdc, usdt, usdg, splErr := services.GetAllSPLBalances(wallet.SolPublicKey)
	if splErr != nil {
		// SPL errors are non-fatal — SOL sync already succeeded.
		// Log the error and continue; user gets SOL updated + whatever SPL succeeded.
		log.Printf("[SyncBalance] SPL balance fetch partial error for %s: %v", wallet.SolPublicKey, splErr)
	}

	if usdc > 0 {
		if err = db.SetBalance(c.Request.Context(), email.(string), "demo_usdc_sol", usdc); err != nil {
			log.Printf("[SyncBalance] Failed to update USDC balance: %v", err)
		}
	}

	if usdt > 0 {
		if err = db.SetBalance(c.Request.Context(), email.(string), "usdt_sol", usdt); err != nil {
			log.Printf("[SyncBalance] Failed to update USDT balance: %v", err)
		}
	}

	if usdg > 0 {
		if err = db.SetBalance(c.Request.Context(), email.(string), "usdg_sol", usdg); err != nil {
			log.Printf("[SyncBalance] Failed to update USDG balance: %v", err)
		}
	}

	log.Printf("[SyncBalance] Sync complete for %s — SOL: %f, USDC: %f, USDT: %f, USDG: %f",
		email, onChainSol, usdc, usdt, usdg)

	balance, _ := db.GetBalanceByEmail(c.Request.Context(), email.(string))
	realNaira, demoNaira := services.ResolveNairaBalances(balance)
	balance.TotalNaira = realNaira
	balance.TotalDemoNaira = demoNaira

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"balance": balance,
	})
}

func SellAsset(c *gin.Context) {
	emailStr, _ := c.Get("email")
	email := emailStr.(string)

	var req struct {
		Asset  string  `json:"asset" binding:"required"`
		Amount float64 `json:"amount" binding:"required"`
		Nature string  `json:"nature" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request"})
		return
	}

	balance, err := db.GetBalanceByEmail(c.Request.Context(), email)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Balance not found"})
		return
	}

	currentAssetBalance := 0.0
	switch req.Asset {
	case "sol":
		currentAssetBalance = balance.Sol
	case "eth_base":
		currentAssetBalance = balance.ETHBase
	case "usdc_sol":
		currentAssetBalance = balance.USDCSol
	case "usdc_base":
		currentAssetBalance = balance.USDCBase
	case "demo_sol":
		currentAssetBalance = balance.DemoSol
	case "demo_usdc_sol":
		currentAssetBalance = balance.DemoUSDCSol
	}

	if req.Amount > currentAssetBalance {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Insufficient balance"})
		return
	}

	receiveNaira := services.GetAssetToNaira(req.Asset, req.Amount)

	if err = db.UpdateBalance(c.Request.Context(), email, req.Asset, -req.Amount); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to initiate deduction"})
		return
	}

	go func() {
		bgCtx := context.Background()
		wallet, err := db.GetWalletByEmail(bgCtx, email)
		if err != nil {
			log.Printf("[Sell] Failed to get wallet for %s, reversing deduction: %v", email, err)
			db.UpdateBalance(bgCtx, email, req.Asset, req.Amount)
			return
		}

		var txHash string
		if req.Asset == "demo_sol" || req.Asset == "sol" {
			decryptedPrivKey, err := services.Decrypt(wallet.SolPrivateKey)
			if err != nil {
				log.Printf("[Sell] Failed to decrypt private key for %s, reversing deduction: %v", email, err)
				db.UpdateBalance(bgCtx, email, req.Asset, req.Amount)
				return
			}

			vaultPubKey := os.Getenv("VANT_SOLANA_VAULT_PUBLIC_KEY")
			txHash, err = services.TransferSol(decryptedPrivKey, vaultPubKey, req.Amount)
			if err != nil {
				log.Printf("[Sell] On-chain transfer failed for %s, reversing deduction: %v", email, err)
				db.UpdateBalance(bgCtx, email, req.Asset, req.Amount)
				return
			}
		}

		nairaField := "naira"
		if req.Nature == "demo" {
			nairaField = "demo_naira"
		}

		if err = db.UpdateBalance(bgCtx, email, nairaField, receiveNaira); err != nil {
			log.Printf("[Sell] CRITICAL: failed to credit USD after successful on-chain move for %s: %v", email, err)
		}

		transaction := models.Transaction{
			ID:        fmt.Sprintf("TX_%s", utils.RandomAlphanumeric(12)),
			UserEmail: email,
			Amount:    receiveNaira,
			Currency:  "USD",
			Nature:    req.Nature,
			Type:      "sell",
			Status:    "completed",
			TxHash:    txHash,
			CreatedAt: time.Now(),
		}
		db.SaveTransaction(bgCtx, transaction)

		go func(toEmail string, tx models.Transaction) {
			if err := services.SendTransactionEmail(toEmail, tx); err != nil {
				log.Printf("[Email] Failed to send sell email to %s (txID: %s): %v", toEmail, tx.ID, err)
			}
		}(email, transaction)

		services.PriceHub.BroadcastToUser(email, "BALANCE_UPDATE")
	}()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Sell initiated successfully",
	})
}

func FundDemoAccount(c *gin.Context) {
	emailStr, _ := c.Get("email")
	email := emailStr.(string)

	var req struct {
		AmountNaira float64 `json:"amount" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request"})
		return
	}

	if req.AmountNaira > 200 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Max request is $200 USD"})
		return
	}

	balance, err := db.GetBalanceByEmail(c.Request.Context(), email)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Balance not found"})
		return
	}

	_, currentDemoUSD := services.ResolveUSDBalances(balance)
	if currentDemoUSD >= 1.0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "You must have less than $1.00 to request demo funds"})
		return
	}

	wallet, err := db.GetWalletByEmail(c.Request.Context(), email)
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
		log.Printf("[Faucet] FundDemoAccount failed for %s: %v", email, err)
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Faucet error: " + err.Error()})
		return
	}

	if err = db.UpdateBalance(c.Request.Context(), email, "demo_sol", amountSol); err != nil {
		log.Printf("[Faucet] Failed to update balance after faucet for %s: %v", email, err)
	}

	transaction := models.Transaction{
		ID:        fmt.Sprintf("TX_%s", utils.RandomAlphanumeric(12)),
		UserEmail: email,
		Amount:    req.AmountNaira,
		Currency:  "USD",
		Nature:    "demo",
		Type:      "faucet",
		Status:    "completed",
		TxHash:    sig,
		CreatedAt: time.Now(),
	}

	if err = db.SaveTransaction(c.Request.Context(), transaction); err != nil {
		log.Printf("[Faucet] Failed to save faucet transaction for %s: %v", email, err)
	}

	go func(toEmail string, tx models.Transaction) {
		if err := services.SendTransactionEmail(toEmail, tx); err != nil {
			log.Printf("[Email] Failed to send faucet email to %s (txID: %s): %v", toEmail, tx.ID, err)
		}
	}(email, transaction)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("Funded %f SOL (~$%.2f USD)", amountSol, req.AmountNaira),
		"tx_hash": sig,
	})
}