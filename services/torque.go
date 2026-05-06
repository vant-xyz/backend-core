package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/vant-xyz/backend-code/db"
)

var getWalletByEmailFn = db.GetWalletByEmail
var callProtectedVASEndpointFn = callProtectedVASEndpoint

type SendTorqueEventRequest struct {
	UserPubkey     string                 `json:"userPubkey"`
	EventName      string                 `json:"eventName"`
	Data           map[string]interface{} `json:"data,omitempty"`
	Timestamp      int64                  `json:"timestamp,omitempty"`
	IdempotencyKey string                 `json:"idempotencyKey,omitempty"`
}

func EmitTorqueEventByEmail(ctx context.Context, email, eventName, idempotencyKey string, data map[string]interface{}) error {
	wallet, err := getWalletByEmailFn(ctx, email)
	if err != nil {
		return fmt.Errorf("wallet lookup failed for %s: %w", email, err)
	}

	pubkey := strings.TrimSpace(wallet.SolPublicKey)
	if pubkey == "" {
		pubkey = strings.TrimSpace(wallet.BasePublicKey)
	}
	if pubkey == "" {
		return fmt.Errorf("no wallet pubkey found for %s", email)
	}

	return EmitTorqueEvent(SendTorqueEventRequest{
		UserPubkey:     pubkey,
		EventName:      eventName,
		Data:           data,
		Timestamp:      time.Now().UnixMilli(),
		IdempotencyKey: idempotencyKey,
	})
}

func EmitTorqueEvent(req SendTorqueEventRequest) error {
	if req.UserPubkey == "" || req.EventName == "" {
		return fmt.Errorf("userPubkey and eventName are required")
	}
	return callProtectedVASEndpointFn("/torque/events", req)
}
