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

	onChainSol, err := services.GetSolBalance(wallet.SolPublicKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to fetch on-chain balance"})
		return
	}

	err = db.SetBalance(c.Request.Context(), email.(string), "demo_sol", onChainSol)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to update database"})
		return
	}

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
	case "sol": currentAssetBalance = balance.Sol
	case "eth_base": currentAssetBalance = balance.ETHBase
	case "usdc_sol": currentAssetBalance = balance.USDCSol
	case "usdc_base": currentAssetBalance = balance.USDCBase
	case "demo_sol": currentAssetBalance = balance.DemoSol
	case "demo_usdc_sol": currentAssetBalance = balance.DemoUSDCSol
	}

	if req.Amount > currentAssetBalance {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Insufficient balance"})
		return
	}

	receiveNaira := services.GetAssetToNaira(req.Asset, req.Amount)

	err = db.UpdateBalance(c.Request.Context(), email, req.Asset, -req.Amount)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to initiate deduction"})
		return
	}

	go func() {
		bgCtx := context.Background()
		wallet, err := db.GetWalletByEmail(bgCtx, email)
		if err != nil {
			db.UpdateBalance(bgCtx, email, req.Asset, req.Amount)
			return
		}

		var txHash string
		if (req.Asset == "demo_sol" || req.Asset == "sol") {
			decryptedPrivKey, err := services.Decrypt(wallet.SolPrivateKey)
			if err != nil {
				db.UpdateBalance(bgCtx, email, req.Asset, req.Amount)
				return
			}
			
			vaultPubKey := os.Getenv("VANT_SOLANA_VAULT_PUBLIC_KEY")
			txHash, err = services.TransferSol(decryptedPrivKey, vaultPubKey, req.Amount)
			if err != nil {
				db.UpdateBalance(bgCtx, email, req.Asset, req.Amount)
				return
			}
		}

		nairaField := "naira"
		if req.Nature == "demo" {
			nairaField = "demo_naira"
		}

		err = db.UpdateBalance(bgCtx, email, nairaField, receiveNaira)
		if err != nil {
			log.Printf("Fatal: Failed to credit naira after successful on-chain move for %s", email)
		}

		transaction := models.Transaction{
			ID:        fmt.Sprintf("TX_%s", utils.RandomAlphanumeric(12)),
			UserEmail: email,
			Amount:    receiveNaira,
			Currency:  "NGN",
			Nature:    req.Nature,
			Type:      "sell",
			Status:    "completed",
			TxHash:    txHash,
			CreatedAt: time.Now(),
		}
		db.SaveTransaction(bgCtx, transaction)
		
		go services.SendTransactionEmail(email, transaction)
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

	if req.AmountNaira > 20000 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Max request is 20,000 NGN"})
		return
	}

	balance, err := db.GetBalanceByEmail(c.Request.Context(), email)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Balance not found"})
		return
	}

	_, currentDemoNaira := services.ResolveNairaBalances(balance)
	if currentDemoNaira >= 100 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "You must have less than 100 NGN to request funds"})
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
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Faucet error: " + err.Error()})
		return
	}

	err = db.UpdateBalance(c.Request.Context(), email, "demo_sol", amountSol)
	if err != nil {
		log.Printf("Error updating balance after faucet: %v", err)
	}

	transaction := models.Transaction{
		ID:        fmt.Sprintf("TX_%s", utils.RandomAlphanumeric(12)),
		UserEmail: email,
		Amount:    req.AmountNaira,
		Currency:  "NGN",
		Nature:    "demo",
		Type:      "faucet",
		Status:    "completed",
		TxHash:    sig,
		CreatedAt: time.Now(),
	}
	
	err = db.SaveTransaction(c.Request.Context(), transaction)
	if err != nil {
		log.Printf("Error saving faucet transaction: %v", err)
	}
	
	go services.SendTransactionEmail(email, transaction)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("Funded %f SOL (~%.2f NGN)", amountSol, req.AmountNaira),
		"tx_hash": sig,
	})
}
