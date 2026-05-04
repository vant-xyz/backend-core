package models

type Balance struct {
	ID             string  `json:"id" firestore:"id"`
	Email          string  `json:"email" firestore:"email"`
	TotalNaira     float64 `json:"total_usd" firestore:"-"`
	TotalDemoNaira float64 `json:"total_demo_usd" firestore:"-"`
	USDCSol        float64 `json:"usdc_sol" firestore:"usdc_sol"`
	USDCBase       float64 `json:"usdc_base" firestore:"usdc_base"`
	USDTSol        float64 `json:"usdt_sol" firestore:"usdt_sol"`
	USDGSol        float64 `json:"usdg_sol" firestore:"usdg_sol"`
	Pusdsol        float64 `json:"pusd_sol" firestore:"pusd_sol"`
	Sol            float64 `json:"sol" firestore:"sol"`
	ETHBase        float64 `json:"eth_base" firestore:"eth_base"`
	Naira          float64 `json:"naira" firestore:"naira"`
	DemoUSDCSol    float64 `json:"demo_usdc_sol" firestore:"demo_usdc_sol"`
	DemoPusdsol    float64 `json:"demo_pusd_sol" firestore:"demo_pusd_sol"`
	DemoSol        float64 `json:"demo_sol" firestore:"demo_sol"`
	DemoNaira      float64 `json:"demo_naira" firestore:"demo_naira"`
	VNaira         float64 `json:"vusd" firestore:"vnaira"`
	LockedBalance  float64 `json:"locked_balance" firestore:"locked_balance"`
	LockedReal     float64 `json:"locked_balance_real" firestore:"locked_balance_real"`
	LockedDemo     float64 `json:"locked_balance_demo" firestore:"locked_balance_demo"`
}
