package services

import (
	"context"
	"log"
	"math"

	"github.com/vant-xyz/backend-code/db"
)

func DepositVP(usdAmount float64) float64 {
	return math.Min(50.0*math.Pow(1.07, math.Min(usdAmount-1.0, 200.0)), 9000000.0)
}

func WithdrawalVP(usdAmount float64) float64 {
	return math.Min(25.0*math.Pow(1.10, math.Min(usdAmount-1.0, 150.0)), 9000000.0)
}

func AssetSaleVP(usdAmount float64) float64 {
	return math.Min(60.0*math.Pow(1.15, math.Min(usdAmount-1.0, 100.0)), 9000000.0)
}

func AwardDepositPoints(ctx context.Context, userEmail string, isDemo bool, usdAmount float64, refID string) {
	pts := DepositVP(usdAmount)
	if err := db.AwardVanticPoints(ctx, userEmail, isDemo, db.VPDeposit, pts, refID); err != nil {
		log.Printf("[VP] deposit award failed user=%s: %v", userEmail, err)
	}
}

func AwardWithdrawalPoints(ctx context.Context, userEmail string, isDemo bool, usdAmount float64, refID string) {
	pts := WithdrawalVP(usdAmount)
	if err := db.AwardVanticPoints(ctx, userEmail, isDemo, db.VPWithdrawal, pts, refID); err != nil {
		log.Printf("[VP] withdrawal award failed user=%s: %v", userEmail, err)
	}
}

func AwardAssetSalePoints(ctx context.Context, userEmail string, isDemo bool, usdAmount float64, refID string) {
	pts := AssetSaleVP(usdAmount)
	if err := db.AwardVanticPoints(ctx, userEmail, isDemo, db.VPAssetSale, pts, refID); err != nil {
		log.Printf("[VP] asset sale award failed user=%s: %v", userEmail, err)
	}
}
