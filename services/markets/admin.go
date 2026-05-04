package markets

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"strings"

	"github.com/gagliardetto/solana-go"
	"github.com/vant-xyz/backend-code/services"
)

const pusdMainnetMint = "CZzgUBvxaMLwMhVSLgqJn3npmxoTo6nzMNQPAnwtHF3s"

type UntetherResult struct {
	WalletType string                `json:"wallet_type"`
	Wallet     string                `json:"wallet"`
	Swaps      []services.SwapResult `json:"swaps"`
	Error      string                `json:"error,omitempty"`
}

func UntetherReserve(ctx context.Context, walletType string) (*UntetherResult, error) {
	walletType = strings.ToLower(strings.TrimSpace(walletType))
	result := &UntetherResult{
		WalletType: walletType,
		Swaps:      []services.SwapResult{},
	}

	cluster := strings.ToLower(strings.TrimSpace(os.Getenv("SOLANA_CLUSTER")))
	if cluster == "" {
		cluster = "mainnet"
	}
	if cluster != "mainnet" {
		result.Error = "untether reserve is mainnet-only"
		return result, fmt.Errorf("%s", result.Error)
	}

	mainnetRPC := os.Getenv("MAINNET_SOLANA_RPC_URL")
	if mainnetRPC == "" {
		result.Error = "MAINNET_SOLANA_RPC_URL not configured"
		return result, fmt.Errorf("%s", result.Error)
	}

	var privateKey solana.PrivateKey
	var walletPubKey string
	var err error

	switch walletType {
	case "fee":
		pk := os.Getenv("VANTIC_FEE_WALLET_SOL_PRIVATE_KEY")
		if pk == "" {
			result.Error = "VANTIC_FEE_WALLET_SOL_PRIVATE_KEY not set"
			return result, fmt.Errorf("%s", result.Error)
		}
		privateKey, err = parseSolanaPrivateKey(pk)
		if err != nil {
			result.Error = fmt.Sprintf("invalid VANTIC_FEE_WALLET_SOL_PRIVATE_KEY: %v", err)
			return result, fmt.Errorf("%s", result.Error)
		}
		walletPubKey = privateKey.PublicKey().String()
	case "mas":
		pk := os.Getenv("VANT_MARKET_APPROVED_SETLLER_KEYPAIR")
		if pk == "" {
			result.Error = "VANT_MARKET_APPROVED_SETLLER_KEYPAIR not set"
			return result, fmt.Errorf("%s", result.Error)
		}
		privateKey, err = parseSolanaPrivateKey(pk)
		if err != nil {
			result.Error = fmt.Sprintf("invalid VANT_MARKET_APPROVED_SETLLER_KEYPAIR: %v", err)
			return result, fmt.Errorf("%s", result.Error)
		}
		walletPubKey = privateKey.PublicKey().String()
	default:
		result.Error = fmt.Sprintf("unsupported wallet type: %s", walletType)
		return result, fmt.Errorf("%s", result.Error)
	}
	result.Wallet = walletPubKey

	usdcMint := os.Getenv("MAINNET_SOL_USDC_MINT")
	if usdcMint == "" {
		usdcMint = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	}
	usdtMint := os.Getenv("MAINNET_SOL_USDT_MINT")
	if usdtMint == "" {
		usdtMint = "Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BcJman"
	}
	pusdMint := os.Getenv("MAINNET_SOL_PUSD_MINT")
	if pusdMint == "" {
		pusdMint = pusdMainnetMint
	}

	stablesToConvert := []struct {
		ticker   string
		mint     string
		decimals uint8
	}{
		{ticker: "USDC", mint: usdcMint, decimals: 6},
		{ticker: "USDT", mint: usdtMint, decimals: 6},
	}

	log.Printf("[Untether] Starting conversion for %s wallet (%s) to PUSD (%s)", walletType, walletPubKey, pusdMint)

	for _, stable := range stablesToConvert {
		balFloat, fetchErr := services.GetSPLBalance(walletPubKey, stable.mint, mainnetRPC, stable.ticker)
		if fetchErr != nil {
			log.Printf("[Untether] Failed to fetch %s balance for %s: %v", stable.ticker, walletType, fetchErr)
			result.Swaps = append(result.Swaps, services.SwapResult{Asset: stable.ticker, Error: fetchErr.Error()})
			continue
		}

		if balFloat <= 0 {
			log.Printf("[Untether] Skipping %s for %s wallet: zero balance", stable.ticker, walletType)
			continue
		}

		amountBaseUnits := uint64(math.Round(balFloat * math.Pow10(int(stable.decimals))))
		if amountBaseUnits == 0 {
			continue
		}

		log.Printf("[Untether] Swapping %.2f %s from %s wallet (%s) to PUSD", balFloat, stable.ticker, walletType, walletPubKey)
		txHash, swapErr := services.JupiterSwapToTargetMint(ctx, privateKey, stable.mint, pusdMint, amountBaseUnits)
		if swapErr != nil {
			log.Printf("[Untether] Failed to swap %s to PUSD for %s wallet: %v", stable.ticker, walletType, swapErr)
			result.Swaps = append(result.Swaps, services.SwapResult{Asset: stable.ticker, Amount: balFloat, Error: swapErr.Error()})
			continue
		}
		log.Printf("[Untether] Successfully swapped %.2f %s to PUSD. Tx: %s", balFloat, stable.ticker, txHash)
		result.Swaps = append(result.Swaps, services.SwapResult{Asset: stable.ticker, Amount: balFloat, TxHash: txHash})
	}

	return result, nil
}

func DumpWalletToUSDC(ctx context.Context, taker solana.PrivateKey) ([]services.SwapResult, error) {
	return services.DumpWalletToUSDC(ctx, taker)
}

func parseSolanaPrivateKey(raw string) (solana.PrivateKey, error) {
	raw = strings.TrimSpace(raw)
	var keyBytes []byte
	if strings.HasPrefix(raw, "[") {
		if err := json.Unmarshal([]byte(raw), &keyBytes); err == nil {
			return solana.PrivateKey(keyBytes), nil
		}
	}
	return solana.PrivateKeyFromBase58(raw)
}
