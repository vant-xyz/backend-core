package services

import (
	"strconv"
	"strings"

	"github.com/vant-xyz/backend-code/models"
)

func GetStreetRate() float64 {
	prices := GetLatestPrices()
	baseRate, _ := strconv.ParseFloat(prices["USD-NGN"].Price, 64)
	if baseRate == 0 { baseRate = 1550.0 }
	
	streetPremium := 40.0
	return baseRate + streetPremium
}

func ResolveNairaBalances(balance *models.Balance) (float64, float64) {
	prices := GetLatestPrices()
	solRateStr := prices["SOL-USD"].Price
	ethRateStr := prices["ETH-USD"].Price

	solRate, _ := strconv.ParseFloat(solRateStr, 64)
	ethRate, _ := strconv.ParseFloat(ethRateStr, 64)

	if solRate == 0 { solRate = 150.0 }
	if ethRate == 0 { ethRate = 2500.0 }

	streetRate := GetStreetRate()

	realNaira := balance.Naira +
		(balance.Sol * solRate * streetRate) +
		(balance.ETHBase * ethRate * streetRate) +
		((balance.USDCSol + balance.USDCBase + balance.USDTSol + balance.USDGSol) * streetRate)

	demoNaira := balance.DemoNaira +
		(balance.DemoSol * solRate * streetRate) +
		(balance.DemoUSDCSol * streetRate)

	return realNaira, demoNaira
}

func GetVantBuyRate() float64 {
	streetRate := GetStreetRate()
	vantMargin := 20.0
	return streetRate - vantMargin
}

func GetVantSellRate() float64 {
	streetRate := GetStreetRate()
	vantMargin := 20.0
	return streetRate + vantMargin
}

func GetSolToNaira(amountSol float64) float64 {
	prices := GetLatestPrices()
	solRate, _ := strconv.ParseFloat(prices["SOL-USD"].Price, 64)
	if solRate == 0 { solRate = 150.0 }
	
	rate := GetStreetRate()
	return amountSol * solRate * rate
}

func GetNairaToSol(amountNaira float64) float64 {
	prices := GetLatestPrices()
	solRate, _ := strconv.ParseFloat(prices["SOL-USD"].Price, 64)
	if solRate == 0 { solRate = 150.0 }
	
	rate := GetStreetRate()
	if rate == 0 || solRate == 0 { return 0 }
	return amountNaira / (solRate * rate)
}

func GetAssetToNaira(asset string, amount float64) float64 {
	prices := GetLatestPrices()
	buyRate := GetVantBuyRate()
	
	normalized := strings.ReplaceAll(asset, "-", "_")
	
	switch normalized {
	case "sol", "demo_sol":
		rate, _ := strconv.ParseFloat(prices["SOL-USD"].Price, 64)
		if rate == 0 { rate = 150.0 }
		return amount * rate * buyRate
	case "eth_base":
		rate, _ := strconv.ParseFloat(prices["ETH-USD"].Price, 64)
		if rate == 0 { rate = 2500.0 }
		return amount * rate * buyRate
	case "usdc_sol", "usdc_base", "usdt_sol", "usdg_sol", "demo_usdc_sol", "usdc", "usdt", "usdg":
		return amount * buyRate
	default:
		return 0
	}
}
