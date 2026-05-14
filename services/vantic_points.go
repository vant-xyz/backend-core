package services

import (
	"context"
	"log"
	"math"

	"github.com/vant-xyz/backend-code/db"
)

func DepositVP(usdAmount float64) float64 {
	return 50.0 * math.Pow(1.1, usdAmount-1.0)
}

func WithdrawalVP(usdAmount float64) float64 {
	return 25.0 * math.Pow(1.3, usdAmount-1.0)
}

func AssetSaleVP(usdAmount float64) float64 {
	return 60.0 * math.Pow(1.7, usdAmount-1.0)
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
