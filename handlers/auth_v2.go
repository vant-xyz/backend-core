package handlers

import (
	"fmt"
	"net/http"

	"github.com/gagliardetto/solana-go"
	"github.com/gin-gonic/gin"
	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/services"
)

func signInMessage(nonce string) string {
	return fmt.Sprintf("Sign in to Vantic\n\nNonce: %s", nonce)
}

// GetNonce issues a one-time nonce for a wallet address.
// GET /v2/auth/nonce?address=<base58-pubkey>
func GetNonce(c *gin.Context) {
	address := c.Query("address")
	if address == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "address query param required"})
		return
	}

	if _, err := solana.PublicKeyFromBase58(address); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid Solana address"})
		return
	}

	nonce, err := services.GenerateNonce()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to generate nonce"})
		return
	}

	if err := services.StoreNonce(c.Request.Context(), address, nonce); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to store nonce"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"nonce":   nonce,
		"message": signInMessage(nonce),
	})
}

// VerifyWallet verifies a signed nonce, then returns a JWT for the wallet owner.
// POST /v2/auth/verify
// Body: { address: string, signature: string }
func VerifyWallet(c *gin.Context) {
	var req struct {
		Address   string `json:"address" binding:"required"`
		Signature string `json:"signature" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "address and signature required"})
		return
	}

	nonce, err := services.GetAndDeleteNonce(c.Request.Context(), req.Address)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "nonce not found or expired — request a new one"})
		return
	}

	message := signInMessage(nonce)
	valid, err := services.VerifyWalletSignature(req.Address, message, req.Signature)
	if err != nil || !valid {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "invalid signature"})
		return
	}

	user, err := db.GetUserByWalletPubkey(c.Request.Context(), req.Address)
	if err != nil {
		user, err = db.CreateV2User(c.Request.Context(), req.Address)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to create user"})
			return
		}
	}

	token, err := services.GenerateV2JWT(req.Address)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"token":   token,
		"user":    user,
	})
}
