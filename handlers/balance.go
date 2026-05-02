package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gin-gonic/gin"
	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/models"
	"github.com/vant-xyz/backend-code/services"
	"github.com/vant-xyz/backend-code/services/markets"
	"github.com/vant-xyz/backend-code/utils"
)

func WithdrawBalance(c *gin.Context) {
	emailStr, _ := c.Get("email")
	email := emailStr.(string)

	var req struct {
		Amount             float64 `json:"amount" binding:"required"`
		DestinationAddress string  `json:"destination_address" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request"})
		return
	}
	if req.Amount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Amount must be positive"})
		return
	}
	withdrawChain := feeChainSolana
	if strings.HasPrefix(strings.ToLower(req.DestinationAddress), "0x") {
		withdrawChain = feeChainBase
	}
	withdrawRate := feeRateForWithdraw(withdrawChain)
	netPayout, feeAmount := applyFee(req.Amount, withdrawRate)
	if netPayout <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Amount too small after fee"})
		return
	}

	if err := db.RunBalanceTransaction(c.Request.Context(), email, func(balance *models.Balance) error {
		if balance.Naira < req.Amount {
			return fmt.Errorf("insufficient balance: available=%.2f requested=%.2f", balance.Naira, req.Amount)
		}
		balance.Naira -= req.Amount
		return nil
	}); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

		reverse := func() {
			db.RunBalanceTransaction(context.Background(), email, func(b *models.Balance) error {
				b.Naira += req.Amount
				return nil
			})
		}

		sig, err := markets.WithdrawFunds(bgCtx, req.DestinationAddress, netPayout)
		if err != nil {
			log.Printf("[Withdraw] Private payment failed for %s to %s: %v — reversing deduction", email, req.DestinationAddress, err)
			reverse()
			return
		}

		log.Printf("[Withdraw] Private payment sent for %s to %s sig=%s gross=%.8f fee=%.8f net=%.8f fee_wallet=%s",
			email, req.DestinationAddress, sig, req.Amount, feeAmount, netPayout, feeWalletForChain(withdrawChain))

		transaction := models.Transaction{
			ID:        fmt.Sprintf("TX_%s", utils.RandomAlphanumeric(12)),
			UserEmail: email,
			Amount:    req.Amount,
			FeeAmount: feeAmount,
			FeeRate:   withdrawRate,
			FeeChain:  string(withdrawChain),
			FeeWallet: feeWalletForChain(withdrawChain),
			Currency:  "USD",
			Nature:    "real",
			Type:      "withdrawal",
			Status:    "completed",
			TxHash:    sig,
			CreatedAt: time.Now(),
		}
		if err := db.SaveTransaction(context.Background(), transaction); err != nil {
			log.Printf("[Withdraw] Failed to save transaction record for %s: %v", email, err)
		}

		services.PriceHub.BroadcastToUser(email, "BALANCE_UPDATE")
	}()

	c.JSON(http.StatusOK, gin.H{
		"success":      true,
		"message":      "Withdrawal initiated",
		"gross_amount": req.Amount,
		"fee_amount":   feeAmount,
		"net_amount":   netPayout,
		"fee_wallet":   feeWalletForChain(withdrawChain),
	})
}

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

func GetUserWSOLBalance(c *gin.Context) {
	email, _ := c.Get("email")

	wallet, err := db.GetWalletByEmail(c.Request.Context(), email.(string))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Wallet not found"})
		return
	}

	devnetWSOL, mainnetWSOL, balErr := services.GetWSOLBalances(wallet.SolPublicKey)
	if balErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to fetch WSOL balances: " + balErr.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"wallet":  wallet.SolPublicKey,
		"wsol": gin.H{
			"devnet":  devnetWSOL,
			"mainnet": mainnetWSOL,
		},
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

	grossReceiveNaira := services.GetAssetToNaira(req.Asset, req.Amount)
	sellChain := chainFromAsset(req.Asset)
	sellRate := feeRateForSell(req.Asset)
	netReceiveNaira, feeAmount := applyFee(grossReceiveNaira, sellRate)
	if netReceiveNaira <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Amount too small after fee"})
		return
	}

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
		} else if req.Asset == "eth_base" || req.Asset == "usdc_base" {
			txHash, err = services.TransferBaseAssetToVault(wallet.BasePrivateKey, req.Asset, req.Amount)
			if err != nil {
				log.Printf("[Sell] Base vault transfer failed for %s asset=%s, reversing deduction: %v", email, req.Asset, err)
				db.UpdateBalance(bgCtx, email, req.Asset, req.Amount)
				return
			}
		}

		nairaField := "naira"
		if req.Nature == "demo" {
			nairaField = "demo_naira"
		}

		if err = db.UpdateBalance(bgCtx, email, nairaField, netReceiveNaira); err != nil {
			log.Printf("[Sell] CRITICAL: failed to credit USD after successful on-chain move for %s: %v", email, err)
		}
		log.Printf("[Sell] Fee applied email=%s asset=%s gross=%.8f fee=%.8f net=%.8f fee_wallet=%s",
			email, req.Asset, grossReceiveNaira, feeAmount, netReceiveNaira, feeWalletForChain(sellChain))

		transaction := models.Transaction{
			ID:        fmt.Sprintf("TX_%s", utils.RandomAlphanumeric(12)),
			UserEmail: email,
			Amount:    netReceiveNaira,
			FeeAmount: feeAmount,
			FeeRate:   sellRate,
			FeeChain:  string(sellChain),
			FeeWallet: feeWalletForChain(sellChain),
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
		"success":       true,
		"message":       "Sell initiated successfully",
		"gross_receive": grossReceiveNaira,
		"fee_amount":    feeAmount,
		"net_receive":   netReceiveNaira,
		"fee_wallet":    feeWalletForChain(sellChain),
	})
}

func ConvertToUSDC(c *gin.Context) {
	emailStr, _ := c.Get("email")
	email := emailStr.(string)

	wallet, err := db.GetWalletByEmail(c.Request.Context(), email)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Wallet not found"})
		return
	}

	decPriv, err := services.Decrypt(wallet.SolPrivateKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Key unavailable"})
		return
	}

	w, err := solana.WalletFromPrivateKeyBase58(decPriv)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Key parse error"})
		return
	}

	results, err := services.DumpWalletToUSDC(c.Request.Context(), w.PrivateKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "swaps": results})
}

func assetBalance(b *models.Balance, asset string) float64 {
	switch asset {
	case "sol":
		return b.Sol
	case "usdc_sol":
		return b.USDCSol
	case "usdt_sol":
		return b.USDTSol
	case "usdg_sol":
		return b.USDGSol
	case "eth_base":
		return b.ETHBase
	case "usdc_base":
		return b.USDCBase
	default:
		return 0
	}
}

func WithdrawAsset(c *gin.Context) {
	emailStr, _ := c.Get("email")
	email := emailStr.(string)

	var req struct {
		Asset              string  `json:"asset" binding:"required"`
		Amount             float64 `json:"amount" binding:"required"`
		DestinationAddress string  `json:"destination_address" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request"})
		return
	}
	if req.Amount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Amount must be positive"})
		return
	}

	chain := chainFromAsset(req.Asset)
	isBase := chain == feeChainBase
	if isBase && !strings.HasPrefix(strings.ToLower(req.DestinationAddress), "0x") {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Base asset requires a 0x destination address"})
		return
	}
	if !isBase && strings.HasPrefix(strings.ToLower(req.DestinationAddress), "0x") {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Solana asset requires a Solana destination address"})
		return
	}

	wallet, err := db.GetWalletByEmail(c.Request.Context(), email)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Wallet not found"})
		return
	}

	var currentBalance float64
	if req.Asset == "wsol" {
		devnetBal, mainnetBal, err := services.GetWSOLBalances(wallet.SolPublicKey)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to fetch WSOL balance"})
			return
		}
		cluster := os.Getenv("SOLANA_CLUSTER")
		if cluster == "" || cluster == "devnet" || cluster == "testnet" {
			currentBalance = devnetBal
		} else {
			currentBalance = mainnetBal
		}
	} else {
		balance, err := db.GetBalanceByEmail(c.Request.Context(), email)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"message": "Balance not found"})
			return
		}
		currentBalance = assetBalance(balance, req.Asset)
		if currentBalance == 0 && assetBalance(balance, req.Asset) == 0 {
			supported := map[string]bool{"sol": true, "usdc_sol": true, "usdt_sol": true, "usdg_sol": true, "eth_base": true, "usdc_base": true}
			if !supported[req.Asset] {
				c.JSON(http.StatusBadRequest, gin.H{"message": "Unsupported asset"})
				return
			}
		}
	}

	withdrawRate := feeRateForWithdraw(chain)
	netAmount, feeAmount := applyFee(req.Amount, withdrawRate)
	if netAmount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Amount too small after fee"})
		return
	}
	if currentBalance < req.Amount {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("Insufficient %s balance", req.Asset)})
		return
	}

	if req.Asset != "wsol" {
		if err := db.UpdateBalance(c.Request.Context(), email, req.Asset, -req.Amount); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to reserve balance"})
			return
		}
	}

	go func() {
		reverse := func() {
			if req.Asset != "wsol" {
				db.UpdateBalance(context.Background(), email, req.Asset, req.Amount)
			}
		}

		var txHash string
		var txErr error

		switch req.Asset {
		case "sol":
			decPriv, err := services.Decrypt(wallet.SolPrivateKey)
			if err != nil {
				log.Printf("[WithdrawAsset] decrypt failed %s: %v", email, err)
				reverse()
				return
			}
			txHash, txErr = services.TransferSol(decPriv, req.DestinationAddress, netAmount)
		case "usdc_sol", "usdt_sol", "usdg_sol", "wsol":
			decPriv, err := services.Decrypt(wallet.SolPrivateKey)
			if err != nil {
				log.Printf("[WithdrawAsset] decrypt failed %s: %v", email, err)
				reverse()
				return
			}
			userWallet, err := solana.WalletFromPrivateKeyBase58(decPriv)
			if err != nil {
				log.Printf("[WithdrawAsset] key parse failed %s: %v", email, err)
				reverse()
				return
			}
			txHash, txErr = markets.SendPrivateSPLAssetPayment(context.Background(), userWallet.PrivateKey, req.DestinationAddress, req.Asset, netAmount)
		case "eth_base", "usdc_base":
			txHash, txErr = services.TransferBaseAsset(wallet.BasePrivateKey, req.Asset, req.DestinationAddress, netAmount)
		default:
			mint, decimals, rpcURL := services.AssetMintConfig(req.Asset)
			if mint == "" || rpcURL == "" {
				log.Printf("[WithdrawAsset] unconfigured asset %s for %s", req.Asset, email)
				reverse()
				return
			}
			decPriv, err := services.Decrypt(wallet.SolPrivateKey)
			if err != nil {
				log.Printf("[WithdrawAsset] decrypt failed %s: %v", email, err)
				reverse()
				return
			}
			txHash, txErr = services.TransferSPLToken(decPriv, req.DestinationAddress, mint, decimals, netAmount, rpcURL)
		}

		if txErr != nil {
			log.Printf("[WithdrawAsset] transfer failed email=%s asset=%s to=%s: %v", email, req.Asset, req.DestinationAddress, txErr)
			reverse()
			return
		}

		log.Printf("[WithdrawAsset] success email=%s asset=%s to=%s gross=%.8f fee=%.8f net=%.8f tx=%s",
			email, req.Asset, req.DestinationAddress, req.Amount, feeAmount, netAmount, txHash)

		transaction := models.Transaction{
			ID:        fmt.Sprintf("TX_%s", utils.RandomAlphanumeric(12)),
			UserEmail: email,
			Amount:    req.Amount,
			FeeAmount: feeAmount,
			FeeRate:   withdrawRate,
			FeeChain:  string(chain),
			FeeWallet: feeWalletForChain(chain),
			Currency:  req.Asset,
			Nature:    "real",
			Type:      "asset_withdrawal",
			Status:    "completed",
			TxHash:    txHash,
			CreatedAt: time.Now(),
		}
		if err := db.SaveTransaction(context.Background(), transaction); err != nil {
			log.Printf("[WithdrawAsset] failed to save tx record %s: %v", email, err)
		}

		services.PriceHub.BroadcastToUser(email, "BALANCE_UPDATE")
	}()

	c.JSON(http.StatusOK, gin.H{
		"success":      true,
		"message":      "Asset withdrawal initiated",
		"asset":        req.Asset,
		"gross_amount": req.Amount,
		"fee_amount":   feeAmount,
		"net_amount":   netAmount,
		"destination":  req.DestinationAddress,
		"fee_wallet":   feeWalletForChain(chain),
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

	if err = db.UpdateBalance(c.Request.Context(), email, "demo_naira", req.AmountNaira); err != nil {
		log.Printf("[Faucet] Failed to update demo balance after faucet for %s: %v", email, err)
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
