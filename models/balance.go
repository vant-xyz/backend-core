package models

type Balance struct {
	ID           string  `json:"id" firestore:"id"`
	Email        string  `json:"email" firestore:"email"`
	USDCSol      float64 `json:"usdc_sol" firestore:"usdc_sol"`
	USDCBase     float64 `json:"usdc_base" firestore:"usdc_base"`
	USDTSol      float64 `json:"usdt_sol" firestore:"usdt_sol"`
	USDGSol      float64 `json:"usdg_sol" firestore:"usdg_sol"`
	Sol          float64 `json:"sol" firestore:"sol"`
	ETHBase      float64 `json:"eth_base" firestore:"eth_base"`
	Naira        float64 `json:"naira" firestore:"naira"`
	DemoUSDCSol  float64 `json:"demo_usdc_sol" firestore:"demo_usdc_sol"`
	DemoSol      float64 `json:"demo_sol" firestore:"demo_sol"`
	DemoNaira    float64 `json:"demo_naira" firestore:"demo_naira"`
	VNaira       float64 `json:"vnaira" firestore:"vnaira"`
}
