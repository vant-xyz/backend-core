package markets

import (
	"context"
	"log"
	"math"
	"math/rand/v2"
	"time"

	"github.com/vant-xyz/backend-code/models"
)

const (
	minDepthPerSideQty     = 300.0
	replenishThresholdQty  = 150.0
	spreadBlowoutThreshold = 0.25
)

var botEmails = []string{
	"carsonpine@hotmail.com", "quaddavid4@hotmail.com", "vant.charlie@testmail.com",
	"vant.diana@testmail.com", "vant.eve@testmail.com", "vant.frank@testmail.com",
	"vant.grace@testmail.com", "vant.henry@testmail.com", "vant.iris@testmail.com",
	"vant.jack@testmail.com", "vant.lily@testmail.com", "vant.max@testmail.com",
	"vant.nina@testmail.com", "vant.omar@testmail.com", "vant.paul@testmail.com",
	"vant.quinn@testmail.com", "vant.rose@testmail.com", "vant.sam@testmail.com",
	"vant.tina@testmail.com", "vant.uma@testmail.com", "vant.victor@testmail.com",
	"vant.wendy@testmail.com", "vant.xander@testmail.com", "vant.yara@testmail.com",
	"vant.zack@testmail.com", "vant.amber@testmail.com", "vant.blake@testmail.com",
	"vant.cora@testmail.com", "vant.derek@testmail.com", "vant.elena@testmail.com",
}

func StartLiquidityProvider(market *models.Market) {
	if market.MarketType != models.MarketTypeCAPPM {
		return
	}
	go runLiquidityLifecycle(market)
}

func runLiquidityLifecycle(market *models.Market) {
	log.Printf("[Liquidity] Starting lifecycle for %s", market.ID)
	ctx := context.Background()
	seedInitialLiquidity(ctx, market)

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m, err := GetMarketByID(ctx, market.ID)
			if err != nil || m.Status != models.MarketStatusActive {
				log.Printf("[Liquidity] Terminating for %s: market not active", market.ID)
				return
			}
			updateLiquidity(ctx, m)
		case <-time.After(time.Until(market.EndTimeUTC)):
			log.Printf("[Liquidity] Terminating for %s: market expired", market.ID)
			return
		}
	}
}

func seedInitialLiquidity(ctx context.Context, market *models.Market) {
	levels := 5
	basePrice := 0.49

	for i := 0; i < levels; i++ {
		if i*2+1 >= len(botEmails) {
			break
		}

		price := basePrice - (float64(i) * (0.02 + rand.Float64()*0.02))
		if price < 0.01 {
			price = 0.01
		}
		price = math.Round(price*100) / 100

		qty := 50.0 + rand.Float64()*150.0
		qty = math.Round(qty)

		PlaceOrder(ctx, PlaceOrderInput{
			UserEmail: botEmails[i*2],
			MarketID:  market.ID,
			Side:      models.OrderSideYes,
			Type:      models.OrderTypeLimit,
			Price:     price,
			Quantity:  qty,
			IsDemo:    true,
		})
		PlaceOrder(ctx, PlaceOrderInput{
			UserEmail: botEmails[i*2+1],
			MarketID:  market.ID,
			Side:      models.OrderSideNo,
			Type:      models.OrderTypeLimit,
			Price:     price,
			Quantity:  qty,
			IsDemo:    true,
		})
	}
	log.Printf("[Liquidity] Initial seeding complete for %s", market.ID)
}

func updateLiquidity(ctx context.Context, market *models.Market) {
	currentPriceCents, err := GetCurrentPrice(market.Asset)
	if err != nil {
		return
	}

	target := float64(market.TargetPrice)
	current := float64(currentPriceCents)

	volatility := GetATRVolatilityFactor(market.Asset, market.DurationSeconds, 0.005)

	z := (current - target) / (target * volatility)
	if market.Direction == models.DirectionBelow {
		z = -z
	}

	prob := 0.5 * (1 + math.Erf(z/math.Sqrt(2)))

	jitter := (rand.Float64() * 0.04) - 0.02
	prob += jitter

	if prob < 0.05 {
		prob = 0.05
	}
	if prob > 0.95 {
		prob = 0.95
	}

	yesBid := math.Floor(prob*100)/100 - 0.01
	noBid := math.Floor((1.0-prob)*100)/100 - 0.01

	if yesBid < 0.01 {
		yesBid = 0.01
	}
	if noBid < 0.01 {
		noBid = 0.01
	}

	cleanupBotOrders(ctx, market.ID)

	qty := 100.0 + rand.Float64()*400.0
	qty = math.Round(qty)

	PlaceOrder(ctx, PlaceOrderInput{
		UserEmail: botEmails[0],
		MarketID:  market.ID,
		Side:      models.OrderSideYes,
		Type:      models.OrderTypeLimit,
		Price:     yesBid,
		Quantity:  qty,
		IsDemo:    true,
	})

	PlaceOrder(ctx, PlaceOrderInput{
		UserEmail: botEmails[1],
		MarketID:  market.ID,
		Side:      models.OrderSideNo,
		Type:      models.OrderTypeLimit,
		Price:     noBid,
		Quantity:  qty,
		IsDemo:    true,
	})

	applyDepthGuardrails(ctx, market.ID)
	alertOnSpreadBlowout(market.ID)
	log.Printf("[Liquidity] Updated for %s: prob=%.4f (jitter=%.4f) yesBid=%.2f noBid=%.2f qty=%.0f",
		market.ID, prob, jitter, yesBid, noBid, qty)
}

func cleanupBotOrders(ctx context.Context, marketID string) {
	for _, email := range botEmails[:2] {
		orders, err := GetUserOrders(ctx, email, marketID)
		if err != nil {
			continue
		}
		for _, o := range orders {
			if o.Status == models.OrderStatusOpen || o.Status == models.OrderStatusPartiallyFilled {
				CancelOrder(ctx, o.ID, email)
			}
		}
	}
}

func applyDepthGuardrails(ctx context.Context, marketID string) {
	engine := GetMatchingEngine()
	yesAsks := engine.GetDepth(marketID, models.OrderSideYes, "asks")
	noAsks := engine.GetDepth(marketID, models.OrderSideNo, "asks")

	yesDepth := totalDepthQty(yesAsks)
	noDepth := totalDepthQty(noAsks)

	if yesDepth < minDepthPerSideQty || noDepth < minDepthPerSideQty {
		log.Printf("[Guardrail] market=%s low-depth yes=%.2f no=%.2f min=%.2f", marketID, yesDepth, noDepth, minDepthPerSideQty)
	}

	if yesDepth < replenishThresholdQty {
		replenishSide(ctx, marketID, models.OrderSideYes)
	}
	if noDepth < replenishThresholdQty {
		replenishSide(ctx, marketID, models.OrderSideNo)
	}
}

func replenishSide(ctx context.Context, marketID string, side models.OrderSide) {
	price := 0.49
	qty := 150.0 + rand.Float64()*200.0
	email := botEmails[0]
	if side == models.OrderSideNo {
		email = botEmails[1]
	}
	if _, err := PlaceOrder(ctx, PlaceOrderInput{
		UserEmail: email,
		MarketID:  marketID,
		Side:      side,
		Type:      models.OrderTypeLimit,
		Price:     price,
		Quantity:  math.Round(qty),
		IsDemo:    true,
	}); err != nil {
		log.Printf("[Guardrail] replenish failed market=%s side=%s err=%v", marketID, side, err)
	}
}

func alertOnSpreadBlowout(marketID string) {
	engine := GetMatchingEngine()
	bids := engine.GetDepth(marketID, models.OrderSideYes, "bids")
	asks := engine.GetDepth(marketID, models.OrderSideYes, "asks")
	if len(bids) == 0 || len(asks) == 0 {
		return
	}
	spread := asks[0].Price - bids[0].Price
	if spread > spreadBlowoutThreshold {
		log.Printf("[ALERT] market=%s spread-blowout spread=%.4f threshold=%.4f", marketID, spread, spreadBlowoutThreshold)
	}
}

func totalDepthQty(levels []OrderbookLevel) float64 {
	total := 0.0
	for _, l := range levels {
		total += l.Quantity
	}
	return total
}
