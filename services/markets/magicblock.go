package markets

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

const privatePaymentsBase = "https://payments.magicblock.app"

type privatePaymentReq struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Mint        string `json:"mint"`
	Amount      uint64 `json:"amount"`
	FromBalance string `json:"fromBalance"`
	ToBalance   string `json:"toBalance"`
	Visibility  string `json:"visibility"`
	Cluster     string `json:"cluster,omitempty"`
}

func clusterForAsset(asset string) string {
	if strings.HasPrefix(asset, "demo_") {
		return "devnet"
	}
	return "mainnet"
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

func getSPLMintAndDecimalsForPrivate(asset string) (string, uint8, error) {
	isDemo := strings.HasPrefix(asset, "demo_")

	switch asset {
	case "usdc_sol", "demo_usdc_sol":
		if isDemo {
			m := os.Getenv("DEVNET_SOL_USDC_MINT")
			if m == "" {
				m = getUSDCMint()
			}
			return m, 6, nil
		}
		m := os.Getenv("MAINNET_SOL_USDC_MINT")
		if m == "" {
			return "", 0, fmt.Errorf("MAINNET_SOL_USDC_MINT not set")
		}
		return m, 6, nil
	case "usdt_sol":
		m := os.Getenv("MAINNET_SOL_USDT_MINT")
		if m == "" {
			return "", 0, fmt.Errorf("MAINNET_SOL_USDT_MINT not set")
		}
		return m, 6, nil
	case "usdg_sol":
		m := os.Getenv("MAINNET_SOL_USDG_MINT")
		if m == "" {
			return "", 0, fmt.Errorf("MAINNET_SOL_USDG_MINT not set")
		}
		return m, 6, nil
	case "pusd_sol":
		return "CZzgUBvxaMLwMhVSLgqJn3npmxoTo6nzMNQPAnwtHF3s", 6, nil
	case "wsol":
		m := os.Getenv("MAINNET_SOL_WSOL_MINT")
		if m == "" {
			m = "So11111111111111111111111111111111111111112"
		}
		return m, 9, nil
	default:
		return "", 0, fmt.Errorf("unsupported private SPL asset: %s", asset)
	}
}

func getEphemeralRPCURL() string {
	if url := os.Getenv("MAGICBLOCK_EPHEMERAL_RPC_URL"); url != "" {
		return url
	}
	return "https://devnet-eu.magicblock.app"
}

func WithdrawFunds(ctx context.Context, recipientAddress string, usdAmount float64, isDemo bool) (string, error) {
	settlerKey, err := getSettlerKeypair()
	if err != nil {
		return "", fmt.Errorf("settler keypair unavailable: %w", err)
	}
	cluster := "mainnet"
	mint := os.Getenv("MAINNET_SOL_USDC_MINT")
	if isDemo {
		cluster = "devnet"
		mint = getUSDCMint()
	}
	if mint == "" {
		return "", fmt.Errorf("USDC mint not configured for cluster %s", cluster)
	}
	units := uint64(usdAmount * 1_000_000)
	return sendPrivateSPLPayment(ctx, settlerKey, recipientAddress, mint, units, cluster)
}

func SendPrivateSPLAssetPayment(ctx context.Context, payerKeypair solana.PrivateKey, recipientAddress, asset string, amount float64) (string, error) {
	if amount <= 0 {
		return "", fmt.Errorf("amount must be positive")
	}
	mint, decimals, err := getSPLMintAndDecimalsForPrivate(asset)
	if err != nil {
		return "", err
	}
	baseUnits := uint64(math.Round(amount * math.Pow10(int(decimals))))
	if baseUnits == 0 {
		return "", fmt.Errorf("amount too small")
	}
	return sendPrivateSPLPayment(ctx, payerKeypair, recipientAddress, mint, baseUnits, clusterForAsset(asset))
}

func sendPrivateSPLPayment(ctx context.Context, payerKeypair solana.PrivateKey, recipientAddress, mint string, amount uint64, cluster string) (string, error) {
	reqBody := privatePaymentReq{
		From:        payerKeypair.PublicKey().String(),
		To:          recipientAddress,
		Mint:        mint,
		Amount:      amount,
		FromBalance: "base",
		ToBalance:   "base",
		Visibility:  "private",
		Cluster:     cluster,
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

	var rpcURLs []string
	if payResp.SendTo == "ephemeral" {
		rpcURLs = []string{getEphemeralRPCURL()}
	} else {
		// sendTo == "base" means submit to standard Solana RPC, not MagicBlock's router
		rpcURLs = getFallbackRPCURLs()
	}

	for _, rpcURL := range rpcURLs {
		sendCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
		rpcClient := rpc.New(rpcURL)
		sig, sendErr := rpcClient.SendTransactionWithOpts(sendCtx, tx, rpc.TransactionOpts{
			SkipPreflight: true,
		})
		cancel()
		if sendErr != nil {
			continue
		}
		return sig.String(), nil
	}

	return "", fmt.Errorf("all RPC endpoints failed for private payment")
}
