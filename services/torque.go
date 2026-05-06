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

var blockedTorqueEmailPrefixes = []string{
	"vant.",
	"test_",
}

var blockedTorqueEmailDomains = []string{
	"@testmail.com",
}

var blockedTorqueExactEmails = map[string]struct{}{
	"carsonpine@hotmail.com": {},
	"quaddavid4@hotmail.com": {},
}

type SendTorqueEventRequest struct {
	UserPubkey     string                 `json:"userPubkey"`
	EventName      string                 `json:"eventName"`
	Data           map[string]interface{} `json:"data,omitempty"`
	Timestamp      int64                  `json:"timestamp,omitempty"`
	IdempotencyKey string                 `json:"idempotencyKey,omitempty"`
}

func EmitTorqueEventByEmail(ctx context.Context, email, eventName, idempotencyKey string, data map[string]interface{}) error {
	if isBlockedTorqueEmail(email) {
		return nil
	}

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

func isBlockedTorqueEmail(email string) bool {
	e := strings.ToLower(strings.TrimSpace(email))
	if e == "" {
		return true
	}
	if _, ok := blockedTorqueExactEmails[e]; ok {
		return true
	}
	for _, d := range blockedTorqueEmailDomains {
		if strings.HasSuffix(e, d) {
			return true
		}
	}
	for _, p := range blockedTorqueEmailPrefixes {
		if strings.HasPrefix(e, p) {
			return true
		}
	}
	return false
}
