package services

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

const (
	jupiterBase     = "https://api.jup.ag"
	mainnetUSDCMint = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	mainnetUSDTMint = "Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BcJman"
	wormholeETHMint = "7vfCXTUXx5WJV5JADk17DUJ4ksgau7utNKj4b963voxs"
	solKeepBuffer   = uint64(10_000_000)
)

type SwapResult struct {
	Asset  string  `json:"asset"`
	Amount float64 `json:"amount"`
	TxHash string  `json:"tx_hash,omitempty"`
	Error  string  `json:"error,omitempty"`
}

func ResolveTickerMint(ticker string) string {
	switch strings.ToUpper(ticker) {
	case "SOL":
		return defaultWSOLMint
	case "USDC", "USDC_SOL", "USDC_BASE":
		if m := os.Getenv("MAINNET_SOL_USDC_MINT"); m != "" {
			return m
		}
		return mainnetUSDCMint
	case "USDT", "USDT_SOL":
		if m := os.Getenv("MAINNET_SOL_USDT_MINT"); m != "" {
			return m
		}
		return mainnetUSDTMint
	case "USDG", "USDG_SOL":
		return os.Getenv("MAINNET_SOL_USDG_MINT")
	case "ETH", "ETH_BASE":
		return wormholeETHMint
	default:
		return ""
	}
}

type priceEntry struct {
	USDPrice       float64 `json:"usdPrice"`
	PriceChange24h float64 `json:"priceChange24h"`
}

func GetTokenPrices(tickers []string) (map[string]float64, error) {
	mintToTicker := map[string]string{}
	var mints []string
	for _, t := range tickers {
		mint := ResolveTickerMint(t)
		if mint == "" {
			continue
		}
		upper := strings.ToUpper(t)
		if _, seen := mintToTicker[mint]; !seen {
			mintToTicker[mint] = upper
			mints = append(mints, mint)
		}
	}
	if len(mints) == 0 {
		return nil, fmt.Errorf("no valid tickers")
	}

	url := fmt.Sprintf("%s/price/v3?ids=%s", jupiterBase, strings.Join(mints, ","))
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", os.Getenv("JUPITER_API_KEY"))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jupiter price API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("jupiter price API %d: %s", resp.StatusCode, string(body))
	}

	var priceResp map[string]priceEntry
	if err := json.NewDecoder(resp.Body).Decode(&priceResp); err != nil {
		return nil, fmt.Errorf("decode jupiter price response: %w", err)
	}

	result := map[string]float64{}
	for mint, entry := range priceResp {
		if ticker, ok := mintToTicker[mint]; ok {
			result[ticker] = entry.USDPrice
		}
	}
	return result, nil
}

type jupiterOrderResp struct {
	Transaction string `json:"transaction"`
	RequestID   string `json:"requestId"`
}

type jupiterExecuteResp struct {
	TxID   string `json:"txid"`
	Status string `json:"status"`
}

func jupiterSwapToUSDC(ctx context.Context, taker solana.PrivateKey, inputMint string, amountBaseUnits uint64) (string, error) {
	outputMint := mainnetUSDCMint
	if m := os.Getenv("MAINNET_SOL_USDC_MINT"); m != "" {
		outputMint = m
	}

	orderURL := fmt.Sprintf("%s/swap/v2/order?inputMint=%s&outputMint=%s&amount=%d&taker=%s",
		jupiterBase, inputMint, outputMint, amountBaseUnits, taker.PublicKey().String())

	req, err := http.NewRequestWithContext(ctx, "GET", orderURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("x-api-key", os.Getenv("JUPITER_API_KEY"))

	httpClient := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("jupiter order: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("jupiter order %d: %s", resp.StatusCode, string(body))
	}

	var orderResp jupiterOrderResp
	if err := json.NewDecoder(resp.Body).Decode(&orderResp); err != nil {
		return "", fmt.Errorf("decode jupiter order: %w", err)
	}

	txBytes, err := base64.StdEncoding.DecodeString(orderResp.Transaction)
	if err != nil {
		return "", fmt.Errorf("decode jupiter tx: %w", err)
	}

	tx, err := solana.TransactionFromBytes(txBytes)
	if err != nil {
		return "", fmt.Errorf("parse jupiter tx: %w", err)
	}

	if _, err := tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(taker.PublicKey()) {
			return &taker
		}
		return nil
	}); err != nil {
		return "", fmt.Errorf("sign jupiter tx: %w", err)
	}

	signedBytes, err := tx.MarshalBinary()
	if err != nil {
		return "", fmt.Errorf("marshal signed jupiter tx: %w", err)
	}

	execBody, _ := json.Marshal(map[string]string{
		"signedTransaction": base64.StdEncoding.EncodeToString(signedBytes),
		"requestId":         orderResp.RequestID,
	})

	execReq, err := http.NewRequestWithContext(ctx, "POST", jupiterBase+"/swap/v2/execute", bytes.NewReader(execBody))
	if err != nil {
		return "", err
	}
	execReq.Header.Set("Content-Type", "application/json")
	execReq.Header.Set("x-api-key", os.Getenv("JUPITER_API_KEY"))

	execResp, err := httpClient.Do(execReq)
	if err != nil {
		return "", fmt.Errorf("jupiter execute: %w", err)
	}
	defer execResp.Body.Close()

	if execResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(execResp.Body)
		return "", fmt.Errorf("jupiter execute %d: %s", execResp.StatusCode, string(body))
	}

	var execResult jupiterExecuteResp
	if err := json.NewDecoder(execResp.Body).Decode(&execResult); err != nil {
		return "", fmt.Errorf("decode jupiter execute: %w", err)
	}

	if execResult.TxID == "" {
		return "", fmt.Errorf("jupiter execute returned empty txid")
	}

	return execResult.TxID, nil
}

type dumpableAsset struct {
	ticker   string
	mint     string
	decimals uint8
	keepBuf  uint64
}

func getDumpableAssets() []dumpableAsset {
	outputMint := mainnetUSDCMint
	if m := os.Getenv("MAINNET_SOL_USDC_MINT"); m != "" {
		outputMint = m
	}

	candidates := []dumpableAsset{
		{ticker: "SOL", mint: defaultWSOLMint, decimals: 9, keepBuf: solKeepBuffer},
		{ticker: "USDT", mint: mainnetUSDTMint, decimals: 6},
		{ticker: "USDG", mint: os.Getenv("MAINNET_SOL_USDG_MINT"), decimals: 6},
		{ticker: "ETH", mint: wormholeETHMint, decimals: 8},
	}
	if m := os.Getenv("MAINNET_SOL_USDT_MINT"); m != "" {
		candidates[1].mint = m
	}

	result := make([]dumpableAsset, 0, len(candidates))
	for _, a := range candidates {
		if a.mint == "" || a.mint == outputMint {
			continue
		}
		result = append(result, a)
	}
	return result
}

func solBalanceFromRPC(pubKey, rpcURL string) (float64, error) {
	client := rpc.New(rpcURL)
	account, err := solana.PublicKeyFromBase58(pubKey)
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	res, err := client.GetBalance(ctx, account, rpc.CommitmentFinalized)
	if err != nil {
		return 0, err
	}
	return float64(res.Value) / 1e9, nil
}

func DumpWalletToUSDC(ctx context.Context, taker solana.PrivateKey) ([]SwapResult, error) {
	mainnetRPC := os.Getenv("MAINNET_SOLANA_RPC_URL")
	if mainnetRPC == "" {
		return nil, fmt.Errorf("MAINNET_SOLANA_RPC_URL not configured")
	}

	pubKey := taker.PublicKey().String()
	assets := getDumpableAssets()
	results := make([]SwapResult, 0, len(assets))

	for _, asset := range assets {
		var balFloat float64
		var fetchErr error

		if asset.ticker == "SOL" {
			balFloat, fetchErr = solBalanceFromRPC(pubKey, mainnetRPC)
		} else {
			balFloat, fetchErr = GetSPLBalance(pubKey, asset.mint, mainnetRPC, asset.ticker)
		}

		if fetchErr != nil {
			results = append(results, SwapResult{Asset: asset.ticker, Error: fetchErr.Error()})
			continue
		}

		amountUnits := uint64(math.Round(balFloat * math.Pow10(int(asset.decimals))))
		if amountUnits <= asset.keepBuf {
			continue
		}
		amountUnits -= asset.keepBuf

		txHash, swapErr := jupiterSwapToUSDC(ctx, taker, asset.mint, amountUnits)
		if swapErr != nil {
			results = append(results, SwapResult{Asset: asset.ticker, Amount: balFloat, Error: swapErr.Error()})
			continue
		}
		results = append(results, SwapResult{Asset: asset.ticker, Amount: balFloat, TxHash: txHash})
	}

	return results, nil
}
