package handlers

import (
	"encoding/base64"
	"net/http"
	"os"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/gin-gonic/gin"
)

// SubmitTransaction receives a signed base64 transaction from the frontend
// and submits it to Solana via the server-side RPC. The frontend never touches
// the RPC URL — it only signs bytes.
// POST /v2/transactions/submit
// Body: { transaction: string } (base64-encoded signed VersionedTransaction)
func SubmitTransaction(c *gin.Context) {
	var req struct {
		Transaction string `json:"transaction" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "transaction field required"})
		return
	}

	txBytes, err := base64.StdEncoding.DecodeString(req.Transaction)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid transaction encoding"})
		return
	}

	tx, err := solana.TransactionFromBytes(txBytes)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid transaction"})
		return
	}

	rpcURL := os.Getenv("MAINNET_SOLANA_RPC_URL")
	if rpcURL == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "RPC not configured"})
		return
	}

	client := rpc.New(rpcURL)
	sig, err := client.SendTransaction(c.Request.Context(), tx)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": "transaction submission failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"signature": sig.String(),
	})
}
