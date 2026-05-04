package markets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/vant-xyz/backend-code/services"
)

const pusdMainnetMint = "CZzgUBvxaMLwMhVSLgqJn3npmxoTo6nzMNQPAnwtHF3s"
const wormholeETHMainnetMint = "7vfCXTUXx5WJV5JADk17DUJ4ksgau7utNKj4b963voxs"

type UntetherResult struct {
	WalletType string                `json:"wallet_type"`
	Wallet     string                `json:"wallet"`
	Swaps      []services.SwapResult `json:"swaps"`
	Error      string                `json:"error,omitempty"`
}

type ReserveWalletBalancesResult struct {
	Fee     WalletBalances `json:"fee"`
	FeeBase WalletBalances `json:"fee_base"`
	MAS     WalletBalances `json:"mas"`
}

type WalletBalances struct {
	Wallet string             `json:"wallet"`
	Tokens map[string]float64 `json:"tokens"`
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
		usdtMint = "Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB"
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

func GetReserveWalletBalances(ctx context.Context) (*ReserveWalletBalancesResult, error) {
	mainnetRPC := os.Getenv("MAINNET_SOLANA_RPC_URL")
	if strings.TrimSpace(mainnetRPC) == "" {
		return nil, fmt.Errorf("MAINNET_SOLANA_RPC_URL not configured")
	}

	feeRaw := os.Getenv("VANTIC_FEE_WALLET_SOL_PRIVATE_KEY")
	if strings.TrimSpace(feeRaw) == "" {
		return nil, fmt.Errorf("VANTIC_FEE_WALLET_SOL_PRIVATE_KEY not set")
	}
	feeKey, err := parseSolanaPrivateKey(feeRaw)
	if err != nil {
		return nil, fmt.Errorf("invalid VANTIC_FEE_WALLET_SOL_PRIVATE_KEY: %w", err)
	}

	masRaw := os.Getenv("VANT_MARKET_APPROVED_SETLLER_KEYPAIR")
	if strings.TrimSpace(masRaw) == "" {
		return nil, fmt.Errorf("VANT_MARKET_APPROVED_SETLLER_KEYPAIR not set")
	}
	masKey, err := parseSolanaPrivateKey(masRaw)
	if err != nil {
		return nil, fmt.Errorf("invalid VANT_MARKET_APPROVED_SETLLER_KEYPAIR: %w", err)
	}

	feeBalances, err := getMainnetWalletBalances(ctx, feeKey.PublicKey().String(), mainnetRPC)
	if err != nil {
		return nil, err
	}
	masBalances, err := getMainnetWalletBalances(ctx, masKey.PublicKey().String(), mainnetRPC)
	if err != nil {
		return nil, err
	}
	feeBaseBalances, err := getBaseFeeWalletBalances(ctx)
	if err != nil {
		return nil, err
	}

	return &ReserveWalletBalancesResult{
		Fee: WalletBalances{
			Wallet: feeKey.PublicKey().String(),
			Tokens: feeBalances,
		},
		FeeBase: feeBaseBalances,
		MAS: WalletBalances{
			Wallet: masKey.PublicKey().String(),
			Tokens: masBalances,
		},
	}, nil
}

func getMainnetWalletBalances(ctx context.Context, walletPubKey, rpcURL string) (map[string]float64, error) {
	usdcMint := os.Getenv("MAINNET_SOL_USDC_MINT")
	if usdcMint == "" {
		usdcMint = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	}
	usdtMint := os.Getenv("MAINNET_SOL_USDT_MINT")
	if usdtMint == "" {
		usdtMint = "Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB"
	}
	usdgMint := os.Getenv("MAINNET_SOL_USDG_MINT")
	pusdMint := os.Getenv("MAINNET_SOL_PUSD_MINT")
	if pusdMint == "" {
		pusdMint = pusdMainnetMint
	}

	tokens := map[string]float64{}
	solBal, err := getSolBalanceMainnet(ctx, walletPubKey, rpcURL)
	if err != nil {
		return nil, err
	}
	tokens["SOL"] = solBal

	if usdcMint != "" {
		bal, err := services.GetSPLBalance(walletPubKey, usdcMint, rpcURL, "USDC")
		if err != nil {
			return nil, err
		}
		tokens["USDC"] = bal
	}
	if usdtMint != "" {
		bal, err := services.GetSPLBalance(walletPubKey, usdtMint, rpcURL, "USDT")
		if err != nil {
			return nil, err
		}
		tokens["USDT"] = bal
	}
	if usdgMint != "" {
		bal, err := services.GetSPLBalance(walletPubKey, usdgMint, rpcURL, "USDG")
		if err != nil {
			return nil, err
		}
		tokens["USDG"] = bal
	}
	if pusdMint != "" {
		bal, err := services.GetSPLBalance(walletPubKey, pusdMint, rpcURL, "PUSD")
		if err != nil {
			return nil, err
		}
		tokens["PUSD"] = bal
	}
	bal, err := services.GetSPLBalance(walletPubKey, wormholeETHMainnetMint, rpcURL, "ETH")
	if err != nil {
		return nil, err
	}
	tokens["ETH"] = bal

	return tokens, nil
}

func getBaseFeeWalletBalances(ctx context.Context) (WalletBalances, error) {
	out := WalletBalances{
		Wallet: strings.TrimSpace(os.Getenv("VANTIC_FEE_WALLET_BASE")),
		Tokens: map[string]float64{},
	}
	if out.Wallet == "" {
		return out, errors.New("VANTIC_FEE_WALLET_BASE not set")
	}
	baseRPC := strings.TrimSpace(os.Getenv("MAINNET_BASE_RPC_URL"))
	if baseRPC == "" {
		return out, errors.New("MAINNET_BASE_RPC_URL not set")
	}

	client, err := ethclient.Dial(baseRPC)
	if err != nil {
		return out, err
	}
	defer client.Close()

	addr := common.HexToAddress(out.Wallet)
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	weiBal, err := client.BalanceAt(timeoutCtx, addr, nil)
	if err != nil {
		return out, err
	}
	ethFloat, _ := new(big.Float).Quo(new(big.Float).SetInt(weiBal), big.NewFloat(1e18)).Float64()
	out.Tokens["ETH"] = ethFloat

	usdcContractHex := strings.TrimSpace(os.Getenv("MAINNET_BASE_USDC_CONTRACT"))
	if usdcContractHex != "" {
		usdcContract := common.HexToAddress(usdcContractHex)
		methodID := crypto.Keccak256([]byte("balanceOf(address)"))[:4]
		paddedAddr := common.LeftPadBytes(addr.Bytes(), 32)
		data := append(methodID, paddedAddr...)

		callRes, err := client.CallContract(timeoutCtx, ethereum.CallMsg{
			To:   &usdcContract,
			Data: data,
		}, nil)
		if err != nil {
			return out, err
		}
		usdcUnits := new(big.Int).SetBytes(callRes)
		usdcFloat, _ := new(big.Float).Quo(new(big.Float).SetInt(usdcUnits), big.NewFloat(1e6)).Float64()
		out.Tokens["USDC"] = usdcFloat
	}

	return out, nil
}

func getSolBalanceMainnet(ctx context.Context, walletPubKey, rpcURL string) (float64, error) {
	client := rpc.New(rpcURL)
	pub, err := solana.PublicKeyFromBase58(walletPubKey)
	if err != nil {
		return 0, err
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	res, err := client.GetBalance(timeoutCtx, pub, rpc.CommitmentFinalized)
	if err != nil {
		return 0, err
	}
	return float64(res.Value) / 1e9, nil
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
