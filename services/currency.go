package services

import (
	"strconv"

	"github.com/vant-xyz/backend-code/models"
)

func ResolveNairaBalances(balance *models.Balance) (float64, float64) {
	prices := GetLatestPrices()
	
	ngnRateStr := prices["USD-NGN"].Price
	solRateStr := prices["SOL-USD"].Price
	ethRateStr := prices["ETH-USD"].Price

	ngnRate, _ := strconv.ParseFloat(ngnRateStr, 64)
	solRate, _ := strconv.ParseFloat(solRateStr, 64)
	ethRate, _ := strconv.ParseFloat(ethRateStr, 64)

	if ngnRate == 0 {
		ngnRate = 1500.0 // Fallback
	}

	realNaira := balance.Naira +
		(balance.Sol * solRate * ngnRate) +
		(balance.ETHBase * ethRate * ngnRate) +
		((balance.USDCSol + balance.USDCBase + balance.USDTSol + balance.USDGSol) * ngnRate)

	demoNaira := balance.DemoNaira +
		(balance.DemoSol * solRate * ngnRate) +
		(balance.DemoUSDCSol * ngnRate)

	return realNaira, demoNaira
}

func GetSolToNaira(amountSol float64) float64 {
	prices := GetLatestPrices()
	ngnRate, _ := strconv.ParseFloat(prices["USD-NGN"].Price, 64)
	solRate, _ := strconv.ParseFloat(prices["SOL-USD"].Price, 64)
	
	if ngnRate == 0 { ngnRate = 1500.0 }
	return amountSol * solRate * ngnRate
}

func GetNairaToSol(amountNaira float64) float64 {
	prices := GetLatestPrices()
	ngnRate, _ := strconv.ParseFloat(prices["USD-NGN"].Price, 64)
	solRate, _ := strconv.ParseFloat(prices["SOL-USD"].Price, 64)
	
	if ngnRate == 0 || solRate == 0 { return 0 }
	return amountNaira / (solRate * ngnRate)
}
