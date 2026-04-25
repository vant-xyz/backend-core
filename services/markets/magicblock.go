package markets

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	confirm "github.com/gagliardetto/solana-go/rpc/sendAndConfirmTransaction"
	"github.com/gagliardetto/solana-go/rpc/ws"
)

const privatePaymentsBase = "https://payments.magicblock.app"

type privatePaymentReq struct {
	From          string `json:"from"`
	To            string `json:"to"`
	Mint          string `json:"mint"`
	Amount        uint64 `json:"amount"`
	FromBalance   string `json:"fromBalance"`
	ToBalance     string `json:"toBalance"`
	Visibility    string `json:"visibility"`
	Cluster       string `json:"cluster,omitempty"`
}

func getCluster() string {
	if c := os.Getenv("SOLANA_CLUSTER"); c != "" {
		return c
	}
	return "devnet"
}

type privatePaymentResp struct {
	Transaction          string   `json:"transactionBase64"`
	RequiredSigners      []string `json:"requiredSigners"`
	SendTo               string   `json:"sendTo"`
	RecentBlockhash      string   `json:"recentBlockhash"`
	LastValidBlockHeight uint64   `json:"lastValidBlockHeight"`
}

func getUSDCMint() string {
	if mint := os.Getenv("DEVNET_SOL_USDC_MINT"); mint != "" {
		return mint
	}
	return "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"
}

func getEphemeralRPCURL() string {
	if url := os.Getenv("MAGICBLOCK_EPHEMERAL_RPC_URL"); url != "" {
		return url
	}
	return "https://devnet-eu.magicblock.app"
}

func WithdrawFunds(ctx context.Context, recipientAddress string, usdAmount float64) (string, error) {
	settlerKey, err := getSettlerKeypair()
	if err != nil {
		return "", fmt.Errorf("settler keypair unavailable: %w", err)
	}
	units := uint64(usdAmount * 1_000_000)
	return SendPrivatePayment(ctx, settlerKey, recipientAddress, units)
}

func SendPrivatePayment(ctx context.Context, payerKeypair solana.PrivateKey, recipientAddress string, amount uint64) (string, error) {
	reqBody := privatePaymentReq{
		From:        payerKeypair.PublicKey().String(),
		To:          recipientAddress,
		Mint:        getUSDCMint(),
		Amount:      amount,
		FromBalance: "base",
		ToBalance:   "base",
		Visibility:  "private",
		Cluster:     getCluster(),
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal private payment request: %w", err)
	}

	httpClient := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.Post(privatePaymentsBase+"/v1/spl/transfer", "application/json", bytes.NewReader(body))
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
	if payResp.SendTo == "ephemeral" {
		rpcURLs = []string{getEphemeralRPCURL()}
	}

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
