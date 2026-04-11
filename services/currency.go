package services

import (
	"strconv"
	"strings"

	"github.com/vant-xyz/backend-code/models"
)

func ResolveUSDBalances(balance *models.Balance) (float64, float64) {
	prices := GetLatestPrices()
	solRateStr := prices["SOL-USD"].Price
	ethRateStr := prices["ETH-USD"].Price

	solRate, _ := strconv.ParseFloat(solRateStr, 64)
	ethRate, _ := strconv.ParseFloat(ethRateStr, 64)

	if solRate == 0 { solRate = 150.0 }
	if ethRate == 0 { ethRate = 2500.0 }

	realUSD := balance.Naira +
		(balance.Sol * solRate) +
		(balance.ETHBase * ethRate) +
		(balance.USDCSol + balance.USDCBase + balance.USDTSol + balance.USDGSol)

	demoUSD := balance.DemoNaira +
		(balance.DemoSol * solRate) +
		balance.DemoUSDCSol

	return realUSD, demoUSD
}

// ResolveNairaBalances is kept as an alias for backwards compatibility.
// It now returns USD totals.
func ResolveNairaBalances(balance *models.Balance) (float64, float64) {
	return ResolveUSDBalances(balance)
}

func GetAssetToUSD(asset string, amount float64) float64 {
	prices := GetLatestPrices()

	normalized := strings.ReplaceAll(asset, "-", "_")

	switch normalized {
	case "sol", "demo_sol":
		rate, _ := strconv.ParseFloat(prices["SOL-USD"].Price, 64)
		if rate == 0 { rate = 150.0 }
		return amount * rate
	case "eth_base":
		rate, _ := strconv.ParseFloat(prices["ETH-USD"].Price, 64)
		if rate == 0 { rate = 2500.0 }
		return amount * rate
	case "usdc_sol", "usdc_base", "usdt_sol", "usdg_sol", "demo_usdc_sol", "usdc", "usdt", "usdg":
		return amount
	default:
		return 0
	}
}

// GetAssetToNaira is kept as an alias for backwards compatibility.
// It now returns USD value.
func GetAssetToNaira(asset string, amount float64) float64 {
	return GetAssetToUSD(asset, amount)
}

func GetUSDToSol(amountUSD float64) float64 {
	prices := GetLatestPrices()
	solRate, _ := strconv.ParseFloat(prices["SOL-USD"].Price, 64)
	if solRate == 0 { solRate = 150.0 }
	return amountUSD / solRate
}

// GetNairaToSol is kept as an alias for backwards compatibility.
func GetNairaToSol(amount float64) float64 {
	return GetUSDToSol(amount)
}

func GetSolToUSD(amountSol float64) float64 {
	prices := GetLatestPrices()
	solRate, _ := strconv.ParseFloat(prices["SOL-USD"].Price, 64)
	if solRate == 0 { solRate = 150.0 }
	return amountSol * solRate
}

// GetSolToNaira is kept as an alias for backwards compatibility.
func GetSolToNaira(amountSol float64) float64 {
	return GetSolToUSD(amountSol)
}
