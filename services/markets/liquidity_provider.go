package markets

import (
	"context"
	"log"
	"math"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/models"
)

const (
	minDepthPerSideQty     = 300.0
	replenishThresholdQty  = 150.0
	spreadBlowoutThreshold = 0.25
	isMainnet              = false
	devBotPairsPerCycle    = 6
	maxBotSharesPerSide    = 2500.0
)

var takerBotPool []string
var activeProviders sync.Map

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

func init() {
	takerBotPool = botEmails
}

func StartLiquidityProvider(market *models.Market) {
	if isMainnet {
		log.Printf("[Liquidity] Disabled on mainnet for %s", market.ID)
		return
	}

	if _, loaded := activeProviders.LoadOrStore(market.ID, true); loaded {
		return
	}

	// Bots now support both CAPPM and GEM
	go runLiquidityLifecycle(market)
}

func StartGlobalLiquidityManager() {
	if isMainnet {
		return
	}

	ctx := context.Background()
	markets, err := db.GetActiveMarkets(ctx)
	if err != nil {
		log.Printf("[LiquidityManager] Failed to fetch active markets: %v", err)
		return
	}

	log.Printf("[LiquidityManager] Found %d active markets to seed", len(markets))
	for _, m := range markets {
		market := m // copy for closure
		go StartLiquidityProvider(&market)
	}
}

func runLiquidityLifecycle(market *models.Market) {
	log.Printf("[Liquidity] Starting lifecycle for %s", market.ID)
	defer activeProviders.Delete(market.ID)

	ctx := context.Background()
	seedInitialLiquidity(ctx, market)

	if market.MarketType == models.MarketTypeCAPPM {
		go runIntelligentTradingLoop(market)
	} else {
		go runRandomTradingLoop(market)
	}

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
	levels := 10
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

		if _, err := PlaceOrder(ctx, PlaceOrderInput{
			UserEmail: botEmails[i*2],
			MarketID:  market.ID,
			Side:      models.OrderSideYes,
			Type:      models.OrderTypeLimit,
			Price:     price,
			Quantity:  qty,
			IsDemo:    true,
		}); err != nil {
			log.Printf("[Liquidity] seed skip bot=%s side=YES market=%s: %v", botEmails[i*2], market.ID, err)
		}
		if _, err := PlaceOrder(ctx, PlaceOrderInput{
			UserEmail: botEmails[i*2+1],
			MarketID:  market.ID,
			Side:      models.OrderSideNo,
			Type:      models.OrderTypeLimit,
			Price:     price,
			Quantity:  qty,
			IsDemo:    true,
		}); err != nil {
			log.Printf("[Liquidity] seed skip bot=%s side=NO market=%s: %v", botEmails[i*2+1], market.ID, err)
		}
	}
	log.Printf("[Liquidity] Initial seeding complete for %s", market.ID)
	seedInitialTrades(ctx, market)
}

func updateLiquidity(ctx context.Context, market *models.Market) {
	var prob float64
	var err error

	if market.MarketType == models.MarketTypeCAPPM {
		prob, err = computeFairValue(market)
		if err != nil {
			return
		}
	} else {
		// GEM markets just hover around 0.5 fair value
		prob = 0.5
	}

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

	perm := rand.Perm(len(botEmails))
	pairs := devBotPairsPerCycle
	maxPairs := len(botEmails) / 2
	if pairs > maxPairs {
		pairs = maxPairs
	}
	for i := 0; i < pairs; i++ {
		yesBot := botEmails[perm[i*2]]
		noBot := botEmails[perm[i*2+1]]
		qty := 80.0 + rand.Float64()*320.0
		qty = math.Round(qty)
		yesPrice := yesBid - (float64(i) * 0.01)
		noPrice := noBid - (float64(i) * 0.01)
		if yesPrice < 0.01 {
			yesPrice = 0.01
		}
		if noPrice < 0.01 {
			noPrice = 0.01
		}

		if _, err := PlaceOrder(ctx, PlaceOrderInput{
			UserEmail: yesBot,
			MarketID:  market.ID,
			Side:      models.OrderSideYes,
			Type:      models.OrderTypeLimit,
			Price:     yesPrice,
			Quantity:  qty,
			IsDemo:    true,
		}); err != nil {
			log.Printf("[Liquidity] update skip bot=%s side=YES market=%s: %v", yesBot, market.ID, err)
		}

		if _, err := PlaceOrder(ctx, PlaceOrderInput{
			UserEmail: noBot,
			MarketID:  market.ID,
			Side:      models.OrderSideNo,
			Type:      models.OrderTypeLimit,
			Price:     noPrice,
			Quantity:  qty,
			IsDemo:    true,
		}); err != nil {
			log.Printf("[Liquidity] update skip bot=%s side=NO market=%s: %v", noBot, market.ID, err)
		}
	}

	applyDepthGuardrails(ctx, market.ID)
	alertOnSpreadBlowout(market.ID)
	log.Printf("[Liquidity] Updated for %s: prob=%.4f (jitter=%.4f) yesBid=%.2f noBid=%.2f pairs=%d",
		market.ID, prob, jitter, yesBid, noBid, pairs)
}

func fairValueProb(currentCents, targetCents uint64, direction models.MarketDirection, volatility, timeFraction float64) float64 {
	if timeFraction <= 0 {
		timeFraction = 0.001
	} else if timeFraction > 1 {
		timeFraction = 1
	}
	adjustedVol := volatility * math.Sqrt(timeFraction)
	if adjustedVol < 0.0001 {
		adjustedVol = 0.0001
	}
	z := (float64(currentCents) - float64(targetCents)) / (float64(targetCents) * adjustedVol)
	if direction == models.DirectionBelow {
		z = -z
	}
	prob := 0.5 * (1 + math.Erf(z/math.Sqrt2))
	if prob < 0.03 {
		return 0.03
	} else if prob > 0.97 {
		return 0.97
	}
	return prob
}

func computeFairValue(market *models.Market) (float64, error) {
	currentCents, err := GetCurrentPrice(market.Asset)
	if err != nil {
		return 0.5, err
	}
	volatility := GetATRVolatilityFactor(market.Asset, market.DurationSeconds, 0.005)
	timeFraction := time.Until(market.EndTimeUTC).Seconds() / float64(market.DurationSeconds)
	return fairValueProb(currentCents, market.TargetPrice, market.Direction, volatility, timeFraction), nil
}

func seedInitialTrades(ctx context.Context, market *models.Market) {
	count := int(math.Round(float64(len(botEmails)) * 0.7))
	perm := rand.Perm(len(botEmails))
	for i := 0; i < count; i++ {
		bot := botEmails[perm[i]]
		side := models.OrderSideYes
		if i%2 != 0 {
			side = models.OrderSideNo
		}
		qty := math.Round(10 + rand.Float64()*40)
		if _, err := PlaceOrder(ctx, PlaceOrderInput{
			UserEmail: bot,
			MarketID:  market.ID,
			Side:      side,
			Type:      models.OrderTypeMarket,
			Quantity:  qty,
			IsDemo:    true,
		}); err != nil {
			log.Printf("[Liquidity] seed trade skip bot=%s side=%s market=%s: %v", bot, side, market.ID, err)
		}
	}
}

func runIntelligentTradingLoop(market *models.Market) {
	ticker := time.NewTicker(30*time.Second + time.Duration(rand.IntN(15))*time.Second)
	defer ticker.Stop()
	deadline := time.NewTimer(time.Until(market.EndTimeUTC))
	defer deadline.Stop()
	ctx := context.Background()
	for {
		select {
		case <-ticker.C:
			m, err := GetMarketByID(ctx, market.ID)
			if err != nil || m.Status != models.MarketStatusActive {
				return
			}
			intelligentTrade(ctx, m)
		case <-deadline.C:
			return
		}
	}
}

func runRandomTradingLoop(market *models.Market) {
	ticker := time.NewTicker(15*time.Second + time.Duration(rand.IntN(10))*time.Second)
	defer ticker.Stop()
	deadline := time.NewTimer(time.Until(market.EndTimeUTC))
	defer deadline.Stop()
	ctx := context.Background()
	for {
		select {
		case <-ticker.C:
			m, err := GetMarketByID(ctx, market.ID)
			if err != nil || m.Status != models.MarketStatusActive {
				return
			}
			randomTrade(ctx, m)
		case <-deadline.C:
			return
		}
	}
}

func randomTrade(ctx context.Context, market *models.Market) {
	side := models.OrderSideYes
	if rand.IntN(2) == 0 {
		side = models.OrderSideNo
	}

	qty := math.Round(5 + rand.Float64()*45)
	bot := takerBotPool[rand.IntN(len(takerBotPool))]

	pos, err := db.GetUserPositionForMarketSide(ctx, bot, market.ID, side, true)
	if err == nil && pos != nil && pos.Shares >= maxBotSharesPerSide {
		if rand.Float64() < 0.3 {
			sellQty := pos.Shares * 0.5
			log.Printf("[RandomBot] limit reached bot=%s side=%s, selling %.2f shares", bot, side, sellQty)
			_, _, _ = ClosePosition(ctx, ClosePositionInput{
				PositionID: pos.ID,
				UserEmail:  bot,
				Shares:     sellQty,
			})
		}
		return
	}

	if _, err := PlaceOrder(ctx, PlaceOrderInput{
		UserEmail: bot,
		MarketID:  market.ID,
		Side:      side,
		Type:      models.OrderTypeMarket,
		Quantity:  qty,
		IsDemo:    true,
	}); err != nil {
		log.Printf("[RandomBot] skip bot=%s side=%s market=%s: %v", bot, side, market.ID, err)
		return
	}
	log.Printf("[RandomBot] market=%s side=%s qty=%.0f", market.ID, side, qty)
}

func intelligentTrade(ctx context.Context, market *models.Market) {
	fairValue, err := computeFairValue(market)
	if err != nil {
		return
	}

	engine := GetMatchingEngine()
	implied := engine.GetLastTradedPrice(market.ID)
	if implied == 0 {
		yesBids := engine.GetDepth(market.ID, models.OrderSideYes, "bids")
		noBids := engine.GetDepth(market.ID, models.OrderSideNo, "bids")
		if len(yesBids) == 0 || len(noBids) == 0 {
			return
		}
		implied = (yesBids[0].Price + (1 - noBids[0].Price)) / 2
	}

	deviation := fairValue - implied
	if math.Abs(deviation) < 0.05 {
		return
	}

	side := models.OrderSideYes
	if deviation < 0 {
		side = models.OrderSideNo
	}

	qty := math.Round(10 + (math.Abs(deviation)/0.5)*70)
	if qty > 80 {
		qty = 80
	}

	bot := takerBotPool[rand.IntN(len(takerBotPool))]

	pos, err := db.GetUserPositionForMarketSide(ctx, bot, market.ID, side, true)
	if err == nil && pos != nil && pos.Shares >= maxBotSharesPerSide {
		if rand.Float64() < 0.3 {
			sellQty := pos.Shares * 0.5
			log.Printf("[IntelligentBot] limit reached bot=%s side=%s, selling %.2f shares", bot, side, sellQty)
			_, _, _ = ClosePosition(ctx, ClosePositionInput{
				PositionID: pos.ID,
				UserEmail:  bot,
				Shares:     sellQty,
			})
		}
		return
	}

	if _, err := PlaceOrder(ctx, PlaceOrderInput{
		UserEmail: bot,
		MarketID:  market.ID,
		Side:      side,
		Type:      models.OrderTypeMarket,
		Quantity:  qty,
		IsDemo:    true,
	}); err != nil {
		log.Printf("[IntelligentBot] skip bot=%s side=%s market=%s: %v", bot, side, market.ID, err)
		return
	}
	log.Printf("[IntelligentBot] market=%s fair=%.4f implied=%.4f dev=%.4f side=%s qty=%.0f",
		market.ID, fairValue, implied, deviation, side, qty)
}

func cleanupBotOrders(ctx context.Context, marketID string) {
	engine := GetMatchingEngine()
	for _, email := range botEmails {
		for _, o := range engine.GetUserOrders(marketID, email) {
			CancelOrder(ctx, o.ID, email)
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
	email := botEmails[rand.IntN(len(botEmails))]
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
