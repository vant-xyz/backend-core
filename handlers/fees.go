package handlers

import (
	"os"
	"strings"
)

type feeChain string

const (
	feeChainSolana feeChain = "solana"
	feeChainBase   feeChain = "base"
)

func feeWalletSolPubKey() string {
	if v := os.Getenv("VANTIC_FEE_WALLET_SOL"); v != "" {
		return v
	}
	return "EVy3WPB2iZpvh1Dw4DogkzZcVfdVr1tVhtqBpnKNje7G"
}

func feeWalletBaseAddress() string {
	return os.Getenv("VANTIC_FEE_WALLET_BASE")
}

func feeWalletForChain(chain feeChain) string {
	if chain == feeChainBase {
		return feeWalletBaseAddress()
	}
	return feeWalletSolPubKey()
}

func applyFee(amount, rate float64) (net, fee float64) {
	if amount <= 0 || rate <= 0 {
		return amount, 0
	}
	fee = amount * rate
	net = amount - fee
	if net < 0 {
		return 0, amount
	}
	return net, fee
}

func feeRateForDeposit(chain feeChain) float64 {
	switch chain {
	case feeChainBase:
		return 0.005
	default:
		return 0.001
	}
}

func feeRateForWithdraw(chain feeChain) float64 {
	switch chain {
	case feeChainBase:
		return 0.005
	default:
		return 0.0
	}
}

func feeRateForSell(asset string) float64 {
	if isBaseAsset(asset) {
		return 0.001
	}
	return 0.0009
}

func chainFromNetwork(network string) feeChain {
	switch {
	case strings.Contains(strings.ToLower(network), "base"):
		return feeChainBase
	default:
		return feeChainSolana
	}
}

func chainFromAsset(asset string) feeChain {
	if isBaseAsset(asset) {
		return feeChainBase
	}
	return feeChainSolana
}

func isBaseAsset(asset string) bool {
	a := strings.ToLower(asset)
	return strings.Contains(a, "_base") || a == "eth" || a == "eth_base" || a == "usdc_base"
}
