package handlers

import (
	"encoding/base64"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/gin-gonic/gin"
)

// submitResult is the cached outcome of submitting a particular signed tx.
type submitResult struct {
	status    int
	body      gin.H
	expiresAt time.Time
}

// submitCache deduplicates submissions by transaction signature. A signed
// transaction has a deterministic signature, so a double-click (or a frontend
// retry) sends identical bytes; without this the same order could be pushed to
// the RPC twice. Entries are short-lived — just long enough to cover retries
// around a single user action.
var (
	submitCacheMu  sync.Mutex
	submitCacheMap = map[string]submitResult{}
)

const (
	submitCacheTTL    = 90 * time.Second
	confirmTimeout    = 45 * time.Second // ~ blockhash validity window
	confirmPollEvery  = 1500 * time.Millisecond
)

func submitCacheGet(sig string) (submitResult, bool) {
	submitCacheMu.Lock()
	defer submitCacheMu.Unlock()
	r, ok := submitCacheMap[sig]
	if !ok {
		return submitResult{}, false
	}
	if time.Now().After(r.expiresAt) {
		delete(submitCacheMap, sig)
		return submitResult{}, false
	}
	return r, true
}

func submitCacheSet(sig string, status int, body gin.H) {
	submitCacheMu.Lock()
	defer submitCacheMu.Unlock()
	submitCacheMap[sig] = submitResult{status: status, body: body, expiresAt: time.Now().Add(submitCacheTTL)}
	// Opportunistic cleanup so the map can't grow unbounded.
	now := time.Now()
	for k, v := range submitCacheMap {
		if now.After(v.expiresAt) {
			delete(submitCacheMap, k)
		}
	}
}

// SubmitTransaction receives a signed base64 transaction from the frontend,
// submits it to Solana via the server-side RPC, and waits for it to confirm
// (or fail) so the caller gets a definitive outcome rather than a fire-and-forget
// signature. The frontend never touches the RPC URL — it only signs bytes.
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
	if len(tx.Signatures) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "transaction is not signed"})
		return
	}
	sig := tx.Signatures[0]
	sigStr := sig.String()

	// Idempotency: if we've already handled this exact signed tx, replay the result.
	if cached, ok := submitCacheGet(sigStr); ok {
		c.JSON(cached.status, cached.body)
		return
	}

	rpcURL := os.Getenv("MAINNET_SOLANA_RPC_URL")
	if rpcURL == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "RPC not configured"})
		return
	}
	client := rpc.New(rpcURL)

	// Send with preflight enabled so obviously-bad txs fail fast at the RPC.
	_, err = client.SendTransactionWithOpts(c.Request.Context(), tx, rpc.TransactionOpts{
		SkipPreflight:       false,
		PreflightCommitment: rpc.CommitmentConfirmed,
	})
	if err != nil {
		// A resend of an already-processed tx surfaces here; treat as success and
		// fall through to confirmation polling rather than erroring.
		if !isAlreadyProcessed(err) {
			respondAndCache(c, sigStr, http.StatusBadGateway, gin.H{
				"success":   false,
				"signature": sigStr,
				"message":   "transaction submission failed: " + err.Error(),
			})
			return
		}
	}

	// Poll for confirmation until the tx lands, fails, or the blockhash expires.
	deadline := time.Now().Add(confirmTimeout)
	for {
		statuses, sErr := client.GetSignatureStatuses(c.Request.Context(), true, sig)
		if sErr == nil && statuses != nil && len(statuses.Value) > 0 && statuses.Value[0] != nil {
			st := statuses.Value[0]
			if st.Err != nil {
				// Tx landed on-chain but execution failed — definitive, do not retry.
				respondAndCache(c, sigStr, http.StatusBadGateway, gin.H{
					"success":   false,
					"signature": sigStr,
					"message":   "transaction failed on-chain",
					"error":     st.Err,
				})
				return
			}
			if st.ConfirmationStatus == rpc.ConfirmationStatusConfirmed ||
				st.ConfirmationStatus == rpc.ConfirmationStatusFinalized {
				respondAndCache(c, sigStr, http.StatusOK, gin.H{
					"success":            true,
					"signature":          sigStr,
					"confirmationStatus": string(st.ConfirmationStatus),
				})
				return
			}
		}

		if time.Now().After(deadline) {
			// Never confirmed within the blockhash window — caller should rebuild
			// and retry. Not cached: the same signature can't land anymore.
			c.JSON(http.StatusGatewayTimeout, gin.H{
				"success":   false,
				"signature": sigStr,
				"message":   "transaction not confirmed in time; please try again",
			})
			return
		}

		select {
		case <-c.Request.Context().Done():
			c.JSON(http.StatusRequestTimeout, gin.H{"success": false, "signature": sigStr, "message": "request cancelled"})
			return
		case <-time.After(confirmPollEvery):
		}
	}
}

func respondAndCache(c *gin.Context, sig string, status int, body gin.H) {
	submitCacheSet(sig, status, body)
	c.JSON(status, body)
}

// isAlreadyProcessed reports whether an RPC send error indicates the tx was
// already submitted/processed, which we treat as non-fatal (we then confirm it).
func isAlreadyProcessed(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already processed")
}
