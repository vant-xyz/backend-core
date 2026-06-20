package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
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

// latestBlockhash fetches a recent blockhash for building the standalone fee tx.
func latestBlockhash(ctx context.Context) (solana.Hash, error) {
	rpcURL := os.Getenv("MAINNET_SOLANA_RPC_URL")
	if rpcURL == "" {
		return solana.Hash{}, fmt.Errorf("MAINNET_SOLANA_RPC_URL not set")
	}
	res, err := rpc.New(rpcURL).GetLatestBlockhash(ctx, rpc.CommitmentConfirmed)
	if err != nil {
		return solana.Hash{}, err
	}
	if res == nil || res.Value == nil {
		return solana.Hash{}, fmt.Errorf("empty blockhash response")
	}
	return res.Value.Blockhash, nil
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

	// Idempotency: if the client supplies a key, serialize same-key requests and
	// replay the cached result so a double-click can't build two distinct orders.
	// The key is scoped to the wallet so keys can't collide across users.
	var rec *idemRecord
	if k := c.GetHeader("Idempotency-Key"); k != "" {
		rec = orderIdem.getOrCreate(owner + ":" + k)
		rec.mu.Lock()
		defer rec.mu.Unlock()
		if rec.done {
			c.JSON(rec.status, rec.body)
			return
		}
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

	// Vantic fee — a SEPARATE transaction (Jupiter pre-signs its order tx, so we
	// cannot modify it). The client signs both in one prompt and submits the
	// order first; the fee is only charged after the order lands.
	var (
		feeTransaction string
		feeAmount      uint64
	)
	if isBuy && depositAmount > 0 {
		blockhash, bhErr := latestBlockhash(c.Request.Context())
		if bhErr != nil {
			// Don't block the trade on an RPC hiccup — log loudly and let the
			// order proceed without the fee tx this once.
			log.Printf("[v2/orders] fee tx skipped, blockhash fetch failed: %v", bhErr)
		} else {
			feeTransaction, feeAmount, err = jupiterclient.BuildFeeTransfer(owner, depositMint, depositAmount, blockhash)
			if err != nil {
				log.Printf("[v2/orders] fee tx build failed owner=%s deposit=%d: %v", owner, depositAmount, err)
				feeTransaction, feeAmount = "", 0
			}
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

	resp := gin.H{
		"transaction":    jupBody.Transaction, // Jupiter's order tx, unmodified
		"feeTransaction": feeTransaction,       // separate Vantic fee tx ("" if none)
		"txMeta":         jupBody.TxMeta,
		"order":          jupBody.Order,
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
	}

	// Cache the built order under the idempotency key so retries replay it
	// rather than building a second order. Only successful responses are cached;
	// failures above returned early and remain retryable.
	if rec != nil {
		rec.status = http.StatusOK
		rec.body = resp
		rec.done = true
		rec.expiresAt = time.Now().Add(orderIdem.ttl)
	}

	c.JSON(http.StatusOK, resp)
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
