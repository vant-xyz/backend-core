package services

import (
	"context"
	"errors"
	"testing"

	"github.com/vant-xyz/backend-code/models"
)

func TestEmitTorqueEvent_Validation(t *testing.T) {
	err := EmitTorqueEvent(SendTorqueEventRequest{EventName: "vantic_deposit"})
	if err == nil {
		t.Fatal("expected validation error for missing userPubkey")
	}

	err = EmitTorqueEvent(SendTorqueEventRequest{UserPubkey: "pubkey"})
	if err == nil {
		t.Fatal("expected validation error for missing eventName")
	}
}

func TestEmitTorqueEvent_ForwardsToVAS(t *testing.T) {
	orig := callProtectedVASEndpointFn
	defer func() { callProtectedVASEndpointFn = orig }()

	called := false
	callProtectedVASEndpointFn = func(path string, body interface{}) error {
		called = true
		if path != "/torque/events" {
			t.Fatalf("expected path /torque/events, got %s", path)
		}

		req, ok := body.(SendTorqueEventRequest)
		if !ok {
			t.Fatalf("expected SendTorqueEventRequest body, got %T", body)
		}
		if req.UserPubkey != "pubkey123" || req.EventName != "vantic_deposit" {
			t.Fatalf("unexpected payload: %+v", req)
		}
		return nil
	}

	err := EmitTorqueEvent(SendTorqueEventRequest{
		UserPubkey: "pubkey123",
		EventName:  "vantic_deposit",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected forwarding call to be executed")
	}
}

func TestEmitTorqueEventByEmail_UsesSolPubkey(t *testing.T) {
	origLookup := getWalletByEmailFn
	origForward := callProtectedVASEndpointFn
	defer func() {
		getWalletByEmailFn = origLookup
		callProtectedVASEndpointFn = origForward
	}()

	getWalletByEmailFn = func(ctx context.Context, email string) (*models.Wallet, error) {
		return &models.Wallet{Email: email, SolPublicKey: "sol_pub", BasePublicKey: "base_pub"}, nil
	}

	var got SendTorqueEventRequest
	callProtectedVASEndpointFn = func(path string, body interface{}) error {
		got = body.(SendTorqueEventRequest)
		return nil
	}

	err := EmitTorqueEventByEmail(context.Background(), "user@example.com", "vantic_trade_complete", "idem-1", map[string]interface{}{"x": 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.UserPubkey != "sol_pub" {
		t.Fatalf("expected sol pubkey, got %s", got.UserPubkey)
	}
	if got.IdempotencyKey != "idem-1" {
		t.Fatalf("expected idempotency key idem-1, got %s", got.IdempotencyKey)
	}
}

func TestEmitTorqueEventByEmail_FallsBackToBasePubkey(t *testing.T) {
	origLookup := getWalletByEmailFn
	origForward := callProtectedVASEndpointFn
	defer func() {
		getWalletByEmailFn = origLookup
		callProtectedVASEndpointFn = origForward
	}()

	getWalletByEmailFn = func(ctx context.Context, email string) (*models.Wallet, error) {
		return &models.Wallet{Email: email, SolPublicKey: "", BasePublicKey: "base_pub"}, nil
	}

	var got SendTorqueEventRequest
	callProtectedVASEndpointFn = func(path string, body interface{}) error {
		got = body.(SendTorqueEventRequest)
		return nil
	}

	err := EmitTorqueEventByEmail(context.Background(), "user@example.com", "vantic_deposit", "idem-2", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.UserPubkey != "base_pub" {
		t.Fatalf("expected base pubkey, got %s", got.UserPubkey)
	}
}

func TestEmitTorqueEventByEmail_ErrorsWithoutPubkey(t *testing.T) {
	origLookup := getWalletByEmailFn
	origForward := callProtectedVASEndpointFn
	defer func() {
		getWalletByEmailFn = origLookup
		callProtectedVASEndpointFn = origForward
	}()

	getWalletByEmailFn = func(ctx context.Context, email string) (*models.Wallet, error) {
		return &models.Wallet{Email: email, SolPublicKey: "", BasePublicKey: ""}, nil
	}

	called := false
	callProtectedVASEndpointFn = func(path string, body interface{}) error {
		called = true
		return nil
	}

	err := EmitTorqueEventByEmail(context.Background(), "user@example.com", "vantic_deposit", "idem-3", nil)
	if err == nil {
		t.Fatal("expected error when wallet has no pubkey")
	}
	if called {
		t.Fatal("forward call should not happen when no pubkey exists")
	}
}

func TestEmitTorqueEventByEmail_PropagatesLookupError(t *testing.T) {
	origLookup := getWalletByEmailFn
	origForward := callProtectedVASEndpointFn
	defer func() {
		getWalletByEmailFn = origLookup
		callProtectedVASEndpointFn = origForward
	}()

	getWalletByEmailFn = func(ctx context.Context, email string) (*models.Wallet, error) {
		return nil, errors.New("wallet lookup down")
	}

	called := false
	callProtectedVASEndpointFn = func(path string, body interface{}) error {
		called = true
		return nil
	}

	err := EmitTorqueEventByEmail(context.Background(), "user@example.com", "vantic_trade_complete", "idem-4", nil)
	if err == nil {
		t.Fatal("expected wallet lookup error")
	}
	if called {
		t.Fatal("forward call should not happen on wallet lookup error")
	}
}
