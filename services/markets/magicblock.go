package markets

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	confirm "github.com/gagliardetto/solana-go/rpc/sendAndConfirmTransaction"
	"github.com/gagliardetto/solana-go/rpc/ws"
)

const privatePaymentsEndpoint = "https://payments.magicblock.app/api/v1/transfer"

type privatePaymentReq struct {
	Payer    string `json:"payer"`
	To       string `json:"to"`
	Lamports uint64 `json:"lamports"`
}

type privatePaymentResp struct {
	Transaction string `json:"transaction"`
}

func WithdrawFunds(ctx context.Context, recipientAddress string, usdAmount float64) (string, error) {
	settlerKey, err := getSettlerKeypair()
	if err != nil {
		return "", fmt.Errorf("settler keypair unavailable: %w", err)
	}
	units := uint64(usdAmount * 1_000_000)
	return SendPrivatePayment(ctx, settlerKey, recipientAddress, units)
}

func SendPrivatePayment(ctx context.Context, payerKeypair solana.PrivateKey, recipientAddress string, lamports uint64) (string, error) {
	reqBody := privatePaymentReq{
		Payer:    payerKeypair.PublicKey().String(),
		To:       recipientAddress,
		Lamports: lamports,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal private payment request: %w", err)
	}

	httpClient := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.Post(privatePaymentsEndpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("private payment API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("private payment API %d: %s", resp.StatusCode, string(raw))
	}

	var payResp privatePaymentResp
	if err := json.NewDecoder(resp.Body).Decode(&payResp); err != nil {
		return "", fmt.Errorf("decode private payment response: %w", err)
	}

	txBytes, err := base64.StdEncoding.DecodeString(payResp.Transaction)
	if err != nil {
		return "", fmt.Errorf("decode private payment tx: %w", err)
	}

	tx, err := solana.TransactionFromBytes(txBytes)
	if err != nil {
		return "", fmt.Errorf("parse private payment tx: %w", err)
	}

	keyMap := map[solana.PublicKey]*solana.PrivateKey{
		payerKeypair.PublicKey(): &payerKeypair,
	}
	if _, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey { return keyMap[key] }); err != nil {
		return "", fmt.Errorf("sign private payment tx: %w", err)
	}

	rpcURLs := getFallbackRPCURLs()
	for _, rpcURL := range rpcURLs {
		wsURL := strings.Replace(rpcURL, "https://", "wss://", 1)
		wsCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
		wsClient, wsErr := ws.Connect(wsCtx, wsURL)
		if wsErr != nil {
			cancel()
			continue
		}
		rpcClient := rpc.New(rpcURL)
		sig, sendErr := confirm.SendAndConfirmTransaction(wsCtx, rpcClient, wsClient, tx)
		wsClient.Close()
		cancel()
		if sendErr != nil {
			continue
		}
		return sig.String(), nil
	}

	return "", fmt.Errorf("all RPC endpoints failed for private payment")
}
