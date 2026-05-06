package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/vant-xyz/backend-code/models"
)

func TestHandleInternalDeposit_EmitsTorqueEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origUpdate := updateBalanceFn
	origSave := saveTransactionFn
	origEmail := sendTransactionEmailFn
	origEmit := emitTorqueEventByEmailFn
	origSweep := sweepDepositFeeOptimisticFn
	origBroadcast := broadcastBalanceUpdateFn
	defer func() {
		updateBalanceFn = origUpdate
		saveTransactionFn = origSave
		sendTransactionEmailFn = origEmail
		emitTorqueEventByEmailFn = origEmit
		sweepDepositFeeOptimisticFn = origSweep
		broadcastBalanceUpdateFn = origBroadcast
	}()

	updateBalanceFn = func(ctx context.Context, email, asset string, amount float64) error { return nil }
	saveTransactionFn = func(ctx context.Context, transaction models.Transaction) error { return nil }
	sendTransactionEmailFn = func(toEmail string, tx models.Transaction) error { return nil }
	sweepDepositFeeOptimisticFn = func(email, asset, network string, feeAmount float64) {}
	broadcastBalanceUpdateFn = func(email string) {}

	type emitCall struct {
		email string
		event string
		idem  string
		data  map[string]interface{}
	}
	emitCh := make(chan emitCall, 1)
	var once sync.Once
	emitTorqueEventByEmailFn = func(ctx context.Context, email, eventName, idempotencyKey string, data map[string]interface{}) error {
		once.Do(func() {
			emitCh <- emitCall{email: email, event: eventName, idem: idempotencyKey, data: data}
		})
		return nil
	}

	payload := map[string]interface{}{
		"email":   "alice@example.com",
		"asset":   "sol",
		"amount":  10.0,
		"tx_hash": "0xabc",
		"network": "solana-mainnet",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/internal/deposit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	HandleInternalDeposit(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	select {
	case got := <-emitCh:
		if got.email != "alice@example.com" {
			t.Fatalf("expected email alice@example.com, got %s", got.email)
		}
		if got.event != "vantic_deposit" {
			t.Fatalf("expected event vantic_deposit, got %s", got.event)
		}
		if got.idem == "" {
			t.Fatalf("expected non-empty idempotency key")
		}
		if got.data["asset"] != "sol" {
			t.Fatalf("expected asset sol, got %v", got.data["asset"])
		}
		if got.data["txHash"] != "0xabc" {
			t.Fatalf("expected txHash 0xabc, got %v", got.data["txHash"])
		}
		if got.data["network"] != "solana-mainnet" {
			t.Fatalf("expected network solana-mainnet, got %v", got.data["network"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for torque event emission")
	}
}
