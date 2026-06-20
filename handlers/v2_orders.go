package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	jupiterclient "github.com/vant-xyz/backend-code/services/jupiter"
)

// walletPubkey extracts the authenticated wallet address from the Gin context.
// Returns "" if the request is not authenticated as a v2 wallet user.
func walletPubkey(c *gin.Context) string {
	wp, _ := c.Get("wallet_pubkey")
	s, _ := wp.(string)
	return s
}

// CreateOrder builds a Jupiter order transaction, injects the Vantic fee,
// and returns the modified unsigned tx with a fee preview.
// POST /v2/orders
// Auth: v2 JWT required (wallet_pubkey claim)
func CreateOrder(c *gin.Context) {
	owner := walletPubkey(c)
	if owner == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "v2 wallet auth required"})
		return
	}

	var body map[string]interface{}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request body"})
		return
	}

	// Always use the authenticated wallet — never trust ownerPubkey from the client.
	body["ownerPubkey"] = owner

	isBuy, _ := body["isBuy"].(bool)

	// depositAmount as uint64 for fee calculation.
	var depositAmount uint64
	switch v := body["depositAmount"].(type) {
	case float64:
		depositAmount = uint64(v)
	case string:
		fmt.Sscan(v, &depositAmount)
	}

	depositMint, _ := body["depositMint"].(string)
	if depositMint == "" {
		depositMint = jupiterclient.DefaultDepositMint
		body["depositMint"] = depositMint
	}

	reqBytes, err := json.Marshal(body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to marshal request"})
		return
	}

	jupResp, status, err := jupiterclient.Post(c.Request.Context(), "/orders", reqBytes)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": "upstream error: " + err.Error()})
		return
	}
	if status != http.StatusOK {
		c.Data(status, "application/json; charset=utf-8", jupResp)
		return
	}

	var jupBody struct {
		Transaction string          `json:"transaction"`
		TxMeta      json.RawMessage `json:"txMeta"`
		Order       json.RawMessage `json:"order"`
	}
	if err := json.Unmarshal(jupResp, &jupBody); err != nil {
		c.Data(status, "application/json; charset=utf-8", jupResp)
		return
	}

	// Fee injection — only on buy orders with a deposit.
	var (
		modifiedTx = jupBody.Transaction
		feeAmount  uint64
	)
	if isBuy && depositAmount > 0 && jupBody.Transaction != "" {
		modifiedTx, feeAmount, err = jupiterclient.InjectFee(jupBody.Transaction, owner, depositMint, depositAmount)
		if err != nil {
			// Fail the order rather than silently returning Jupiter's tx with no
			// fee. A silent fallback here means we hand the user an order we earn
			// nothing on and never find out. Log loudly and surface a 502.
			log.Printf("[v2/orders] FEE INJECTION FAILED owner=%s market=%v deposit=%d: %v",
				owner, body["marketId"], depositAmount, err)
			c.JSON(http.StatusBadGateway, gin.H{"message": "could not build order, please try again"})
			return
		}
	}

	// Decode order for preview fields.
	var orderFields struct {
		OrderCostUsd           string `json:"orderCostUsd"`
		EstimatedProtocolFeeUsd string `json:"estimatedProtocolFeeUsd"`
		EstimatedVenueFeeUsd   string `json:"estimatedVenueFeeUsd"`
		EstimatedTotalFeeUsd   string `json:"estimatedTotalFeeUsd"`
		NewPayoutUsd           string `json:"newPayoutUsd"`
	}
	_ = json.Unmarshal(jupBody.Order, &orderFields)

	c.JSON(http.StatusOK, gin.H{
		"transaction": modifiedTx,
		"txMeta":      jupBody.TxMeta,
		"order":       jupBody.Order,
		"preview": gin.H{
			"depositAmount":           depositAmount,
			"depositMint":             depositMint,
			"orderCostUsd":            orderFields.OrderCostUsd,
			"estimatedProtocolFeeUsd": orderFields.EstimatedProtocolFeeUsd,
			"estimatedVenueFeeUsd":    orderFields.EstimatedVenueFeeUsd,
			"estimatedJupiterFeeUsd":  orderFields.EstimatedTotalFeeUsd,
			"vanticFeeAmount":         feeAmount,
			"vanticFeeBps":            jupiterclient.FeeBps,
			"newPayoutUsd":            orderFields.NewPayoutUsd,
		},
	})
}

// GetPositions fetches open positions for the authenticated wallet.
// GET /v2/positions
func GetPositions(c *gin.Context) {
	owner := walletPubkey(c)
	if owner == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "v2 wallet auth required"})
		return
	}

	params := url.Values{"ownerPubkey": {owner}}
	mergeQuery(params, c, "marketId", "isYes", "start", "end")
	passthroughGet(c, "/positions", params)
}

// ClosePosition requests an unsigned tx to sell all contracts for a position.
// DELETE /v2/positions/:positionPubkey
func ClosePosition(c *gin.Context) {
	owner := walletPubkey(c)
	if owner == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "v2 wallet auth required"})
		return
	}

	positionPubkey := c.Param("positionPubkey")
	reqBytes, _ := json.Marshal(map[string]string{"ownerPubkey": owner})

	body, status, err := jupiterclient.Delete(c.Request.Context(), "/positions/"+positionPubkey, reqBytes)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": "upstream error: " + err.Error()})
		return
	}
	c.Data(status, "application/json; charset=utf-8", body)
}

// ClaimPosition requests an unsigned tx to claim payout from a winning position.
// POST /v2/positions/:positionPubkey/claim
func ClaimPosition(c *gin.Context) {
	owner := walletPubkey(c)
	if owner == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "v2 wallet auth required"})
		return
	}

	positionPubkey := c.Param("positionPubkey")
	reqBytes, _ := json.Marshal(map[string]string{"ownerPubkey": owner})

	body, status, err := jupiterclient.Post(c.Request.Context(), "/positions/"+positionPubkey+"/claim", reqBytes)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": "upstream error: " + err.Error()})
		return
	}
	c.Data(status, "application/json; charset=utf-8", body)
}
