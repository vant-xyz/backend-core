package markets

import (
	"context"
	"fmt"
	"os"
	"strings"

	ppkit "github.com/DavidNzube101/magicblock-pp-kit-go"
	"github.com/gagliardetto/solana-go"
)

func getEphemeralRPCURL() string {
	if url := os.Getenv("MAGICBLOCK_EPHEMERAL_RPC_URL"); url != "" {
		return url
	}
	return "https://devnet-eu.magicblock.app"
}

func newPPClient() *ppkit.Client {
	return ppkit.New(
		ppkit.WithEphemeralRPC(getEphemeralRPCURL()),
		ppkit.WithRPCURLs(getFallbackRPCURLs()),
	)
}

func clusterForAsset(asset string) string {
	if strings.HasPrefix(asset, "demo_") {
		return "devnet"
	}
	return "mainnet"
}

func getUSDCMint() string {
	if mint := os.Getenv("DEVNET_SOL_USDC_MINT"); mint != "" {
		return mint
	}
	return ppkit.MintUSDCDevnet
}

func getSPLMintAndDecimalsForPrivate(asset string) (string, uint8, error) {
	isDemo := strings.HasPrefix(asset, "demo_")

	switch asset {
	case "usdc_sol", "demo_usdc_sol":
		if isDemo {
			return getUSDCMint(), 6, nil
		}
		m := os.Getenv("MAINNET_SOL_USDC_MINT")
		if m == "" {
			return "", 0, fmt.Errorf("MAINNET_SOL_USDC_MINT not set")
		}
		return m, 6, nil
	case "usdt_sol":
		m := os.Getenv("MAINNET_SOL_USDT_MINT")
		if m == "" {
			return ppkit.MintUSDTMainnet, 6, nil
		}
		return m, 6, nil
	case "usdg_sol":
		m := os.Getenv("MAINNET_SOL_USDG_MINT")
		if m == "" {
			return ppkit.MintUSDGMainnet, 6, nil
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

var immediateSettlement = []ppkit.TransferOption{
	ppkit.WithMinDelayMs(0),
	ppkit.WithMaxDelayMs(0),
	ppkit.WithSplit(1),
	ppkit.WithInitIfMissing(true),
	ppkit.WithInitAtasIfMissing(true),
}

func WithdrawFunds(ctx context.Context, recipientAddress string, usdAmount float64, isDemo bool) (string, error) {
	settlerKey, err := getSettlerKeypair()
	if err != nil {
		return "", fmt.Errorf("settler keypair unavailable: %w", err)
	}
	c := newPPClient()
	if isDemo {
		return c.TransferUSDCDevnet(ctx, []byte(settlerKey), recipientAddress, usdAmount, immediateSettlement...)
	}
	mint := os.Getenv("MAINNET_SOL_USDC_MINT")
	if mint == "" {
		return "", fmt.Errorf("MAINNET_SOL_USDC_MINT not set")
	}
	return c.TransferSPL(ctx, []byte(settlerKey), recipientAddress, mint, usdAmount, 6, "mainnet", immediateSettlement...)
}

func SendPrivateSPLAssetPayment(ctx context.Context, payerKeypair solana.PrivateKey, recipientAddress, asset string, amount float64) (string, error) {
	if amount <= 0 {
		return "", fmt.Errorf("amount must be positive")
	}
	mint, decimals, err := getSPLMintAndDecimalsForPrivate(asset)
	if err != nil {
		return "", err
	}
	return newPPClient().TransferSPL(ctx, []byte(payerKeypair), recipientAddress, mint, amount, decimals, clusterForAsset(asset), immediateSettlement...)
}
