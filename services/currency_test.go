package services

import (
	"testing"

	"github.com/vant-xyz/backend-code/models"
)

// GetLatestPrices returns an empty map when the price poller hasn't started,
// so all functions below fall through to their hardcoded fallback rates:
//   SOL  = 150.0
//   ETH  = 2500.0
//   stablecoins = 1:1

// ── GetAssetToUSD ─────────────────────────────────────────────────────────────

func TestGetAssetToUSD_SOL_UsesFallback(t *testing.T) {
	got := GetAssetToUSD("sol", 2.0)
	want := 2.0 * 150.0
	if got != want {
		t.Errorf("GetAssetToUSD(sol, 2) = %.2f, want %.2f", got, want)
	}
}

func TestGetAssetToUSD_DemoSOL_UsesFallback(t *testing.T) {
	got := GetAssetToUSD("demo_sol", 1.0)
	want := 150.0
	if got != want {
		t.Errorf("GetAssetToUSD(demo_sol, 1) = %.2f, want %.2f", got, want)
	}
}

func TestGetAssetToUSD_ETH_UsesFallback(t *testing.T) {
	got := GetAssetToUSD("eth_base", 1.0)
	want := 2500.0
	if got != want {
		t.Errorf("GetAssetToUSD(eth_base, 1) = %.2f, want %.2f", got, want)
	}
}

func TestGetAssetToUSD_Stablecoins_AreOneDollar(t *testing.T) {
	stables := []string{"usdc_sol", "usdc_base", "usdt_sol", "usdg_sol", "demo_usdc_sol", "usdc", "usdt", "usdg"}
	for _, asset := range stables {
		got := GetAssetToUSD(asset, 100.0)
		if got != 100.0 {
			t.Errorf("GetAssetToUSD(%s, 100) = %.2f, want 100.00 (stablecoin 1:1)", asset, got)
		}
	}
}

func TestGetAssetToUSD_UnknownAsset_ReturnsZero(t *testing.T) {
	got := GetAssetToUSD("DOGE", 1000.0)
	if got != 0 {
		t.Errorf("GetAssetToUSD(DOGE, 1000) = %.2f, want 0 (unsupported)", got)
	}
}

func TestGetAssetToUSD_ZeroAmount(t *testing.T) {
	got := GetAssetToUSD("sol", 0)
	if got != 0 {
		t.Errorf("GetAssetToUSD(sol, 0) = %.2f, want 0", got)
	}
}

// ── GetUSDToSol / GetSolToUSD ─────────────────────────────────────────────────

func TestGetUSDToSol_FallbackRate(t *testing.T) {
	// $150 should buy exactly 1 SOL at fallback rate of $150/SOL.
	got := GetUSDToSol(150.0)
	want := 1.0
	if got != want {
		t.Errorf("GetUSDToSol(150) = %.4f, want %.4f", got, want)
	}
}

func TestGetUSDToSol_HalfSol(t *testing.T) {
	got := GetUSDToSol(75.0)
	want := 0.5
	if got != want {
		t.Errorf("GetUSDToSol(75) = %.4f, want %.4f", got, want)
	}
}

func TestGetSolToUSD_FallbackRate(t *testing.T) {
	got := GetSolToUSD(1.0)
	want := 150.0
	if got != want {
		t.Errorf("GetSolToUSD(1) = %.2f, want %.2f", got, want)
	}
}

func TestGetSolToUSD_GetUSDToSol_Roundtrip(t *testing.T) {
	original := 300.0
	sol := GetUSDToSol(original)
	back := GetSolToUSD(sol)
	if back != original {
		t.Errorf("USD→SOL→USD roundtrip: %.2f → %.6f SOL → %.2f USD", original, sol, back)
	}
}

// ── ResolveUSDBalances ────────────────────────────────────────────────────────

func TestResolveUSDBalances_RealBalanceOnlyUSD(t *testing.T) {
	b := &models.Balance{Naira: 500}
	real, demo := ResolveUSDBalances(b)
	if real != 500 {
		t.Errorf("real USD = %.2f, want 500", real)
	}
	if demo != 0 {
		t.Errorf("demo USD = %.2f, want 0", demo)
	}
}

func TestResolveUSDBalances_SOLConvertsWithFallback(t *testing.T) {
	// 2 SOL at fallback $150 = $300 real
	b := &models.Balance{Sol: 2.0}
	real, _ := ResolveUSDBalances(b)
	want := 2.0 * 150.0
	if real != want {
		t.Errorf("real USD with SOL = %.2f, want %.2f", real, want)
	}
}

func TestResolveUSDBalances_AllRealComponents(t *testing.T) {
	b := &models.Balance{
		Naira:    100,  // $100 USD vault
		Sol:      1,    // 1 SOL × $150 = $150
		ETHBase:  0.1,  // 0.1 ETH × $2500 = $250
		USDCSol:  50,   // $50 stablecoin
		USDCBase: 25,   // $25 stablecoin
		USDTSol:  10,   // $10 stablecoin
		USDGSol:  5,    // $5 stablecoin
	}
	real, demo := ResolveUSDBalances(b)
	want := 100.0 + 150.0 + 250.0 + 50.0 + 25.0 + 10.0 + 5.0 // = 590
	if real != want {
		t.Errorf("real USD = %.2f, want %.2f", real, float64(want))
	}
	if demo != 0 {
		t.Errorf("demo USD should be 0, got %.2f", demo)
	}
}

func TestResolveUSDBalances_AllDemoComponents(t *testing.T) {
	b := &models.Balance{
		DemoNaira:   50,    // $50
		DemoSol:     0.5,   // 0.5 SOL × $150 = $75
		DemoUSDCSol: 25,    // $25
	}
	real, demo := ResolveUSDBalances(b)
	wantDemo := 50.0 + 75.0 + 25.0 // = 150
	if demo != wantDemo {
		t.Errorf("demo USD = %.2f, want %.2f", demo, wantDemo)
	}
	if real != 0 {
		t.Errorf("real USD should be 0, got %.2f", real)
	}
}

func TestResolveUSDBalances_EmptyBalance(t *testing.T) {
	b := &models.Balance{}
	real, demo := ResolveUSDBalances(b)
	if real != 0 || demo != 0 {
		t.Errorf("empty balance: real=%.2f demo=%.2f, want both 0", real, demo)
	}
}

func TestResolveNairaBalances_IsAliasForUSD(t *testing.T) {
	b := &models.Balance{Naira: 200, DemoNaira: 100}
	r1, d1 := ResolveNairaBalances(b)
	r2, d2 := ResolveUSDBalances(b)
	if r1 != r2 || d1 != d2 {
		t.Errorf("ResolveNairaBalances and ResolveUSDBalances returned different values")
	}
}

// ── currencyToField helper ────────────────────────────────────────────────────

func TestCurrencyToField_KnownCurrencies(t *testing.T) {
	cases := []struct {
		currency string
		want     string
	}{
		{"USD", "naira"},
		{"NGN", "naira"},
		{"USD_DEMO", "demo_naira"},
		{"NGN_DEMO", "demo_naira"},
	}
	for _, tc := range cases {
		got := currencyToField(tc.currency)
		if got != tc.want {
			t.Errorf("currencyToField(%s) = %q, want %q", tc.currency, got, tc.want)
		}
	}
}

func TestCurrencyToField_UnknownCurrency_ReturnsEmpty(t *testing.T) {
	unknown := []string{"BTC", "SOL", "ETH", "USDC", "", "eur"}
	for _, c := range unknown {
		got := currencyToField(c)
		if got != "" {
			t.Errorf("currencyToField(%q) = %q, want empty string", c, got)
		}
	}
}

// ── balanceFieldValue / setBalanceField ──────────────────────────────────────

func TestBalanceFieldValue_ReadsCorrectField(t *testing.T) {
	b := &models.Balance{
		Naira:         1000,
		DemoNaira:     500,
		LockedBalance: 250,
	}
	cases := []struct {
		field string
		want  float64
	}{
		{"naira", 1000},
		{"demo_naira", 500},
		{"locked_balance", 250},
		{"unknown_field", 0},
	}
	for _, tc := range cases {
		got := balanceFieldValue(b, tc.field)
		if got != tc.want {
			t.Errorf("balanceFieldValue(b, %q) = %.2f, want %.2f", tc.field, got, tc.want)
		}
	}
}

func TestSetBalanceField_WritesCorrectField(t *testing.T) {
	b := &models.Balance{}
	setBalanceField(b, "naira", 999)
	setBalanceField(b, "demo_naira", 777)
	setBalanceField(b, "locked_balance", 333)

	if b.Naira != 999 {
		t.Errorf("Naira = %.2f, want 999", b.Naira)
	}
	if b.DemoNaira != 777 {
		t.Errorf("DemoNaira = %.2f, want 777", b.DemoNaira)
	}
	if b.LockedBalance != 333 {
		t.Errorf("LockedBalance = %.2f, want 333", b.LockedBalance)
	}
}

func TestSetBalanceField_UnknownField_NoOp(t *testing.T) {
	b := &models.Balance{Naira: 500}
	setBalanceField(b, "unknown_field", 999)
	if b.Naira != 500 {
		t.Errorf("unknown field write should be no-op, Naira changed to %.2f", b.Naira)
	}
}

// ── balance validation guards (no DB required) ────────────────────────────────

func TestLockBalance_NegativeAmount_ReturnsError(t *testing.T) {
	err := LockBalance(nil, "user@test.xyz", -10, "USD")
	if err == nil {
		t.Error("LockBalance with negative amount should return error")
	}
}

func TestLockBalance_ZeroAmount_ReturnsError(t *testing.T) {
	err := LockBalance(nil, "user@test.xyz", 0, "USD")
	if err == nil {
		t.Error("LockBalance with zero amount should return error")
	}
}

func TestUnlockBalance_NegativeAmount_ReturnsError(t *testing.T) {
	err := UnlockBalance(nil, "user@test.xyz", -5, "USD")
	if err == nil {
		t.Error("UnlockBalance with negative amount should return error")
	}
}

func TestDeductLockedBalance_NegativeAmount_ReturnsError(t *testing.T) {
	err := DeductLockedBalance(nil, "user@test.xyz", -1)
	if err == nil {
		t.Error("DeductLockedBalance with negative amount should return error")
	}
}

func TestCreditBalance_NegativeAmount_ReturnsError(t *testing.T) {
	err := CreditBalance(nil, "user@test.xyz", -50, "USD")
	if err == nil {
		t.Error("CreditBalance with negative amount should return error")
	}
}
