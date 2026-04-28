package markets

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/vant-xyz/backend-code/models"
)

const (
	maxGuaranteedStakePerQuote   = 500.0
	maxGuaranteedStakePerMarket  = 5000.0
	maxReservedSharesPerSide     = 10000.0
	maxQuoteAcceptsPerUserMinute = 30
)

type riskState struct {
	mu sync.Mutex

	marketReservedStake  map[string]float64
	marketReservedShares map[string]map[models.OrderSide]float64
	userAcceptWindow     map[string]acceptWindow

	noLiquidityCount map[string]int
}

type acceptWindow struct {
	start time.Time
	count int
}

var globalRiskState = &riskState{
	marketReservedStake:  make(map[string]float64),
	marketReservedShares: make(map[string]map[models.OrderSide]float64),
	userAcceptWindow:     make(map[string]acceptWindow),
	noLiquidityCount:     make(map[string]int),
}

func (r *riskState) canReserve(marketID string, side models.OrderSide, stake, shares float64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if stake > maxGuaranteedStakePerQuote {
		return errf("stake %.2f exceeds max guaranteed quote stake %.2f", stake, maxGuaranteedStakePerQuote)
	}

	if r.marketReservedStake[marketID]+stake > maxGuaranteedStakePerMarket {
		return errf("market guaranteed capacity exceeded")
	}

	if _, ok := r.marketReservedShares[marketID]; !ok {
		r.marketReservedShares[marketID] = map[models.OrderSide]float64{
			models.OrderSideYes: 0,
			models.OrderSideNo:  0,
		}
	}

	if r.marketReservedShares[marketID][side]+shares > maxReservedSharesPerSide {
		return errf("side guaranteed capacity exceeded")
	}

	return nil
}

func (r *riskState) reserve(marketID string, side models.OrderSide, stake, shares float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.marketReservedStake[marketID] += stake
	if _, ok := r.marketReservedShares[marketID]; !ok {
		r.marketReservedShares[marketID] = map[models.OrderSide]float64{
			models.OrderSideYes: 0,
			models.OrderSideNo:  0,
		}
	}
	r.marketReservedShares[marketID][side] += shares
}

func (r *riskState) release(marketID string, side models.OrderSide, stake, shares float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.marketReservedStake[marketID] -= stake
	if r.marketReservedStake[marketID] < 0 {
		r.marketReservedStake[marketID] = 0
	}

	if sideMap, ok := r.marketReservedShares[marketID]; ok {
		sideMap[side] -= shares
		if sideMap[side] < 0 {
			sideMap[side] = 0
		}
	}
}

func (r *riskState) allowAccept(userEmail string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().UTC()
	win := r.userAcceptWindow[userEmail]
	if win.start.IsZero() || now.Sub(win.start) >= time.Minute {
		r.userAcceptWindow[userEmail] = acceptWindow{start: now, count: 1}
		return true
	}
	if win.count >= maxQuoteAcceptsPerUserMinute {
		return false
	}
	win.count++
	r.userAcceptWindow[userEmail] = win
	return true
}

func (r *riskState) recordNoLiquidity(marketID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.noLiquidityCount[marketID]++
	if r.noLiquidityCount[marketID]%10 == 0 {
		log.Printf("[ALERT] market=%s no-liquidity-events=%d", marketID, r.noLiquidityCount[marketID])
	}
}

func errf(format string, args ...interface{}) error {
	return &riskErr{msg: fmt.Sprintf(format, args...)}
}

type riskErr struct {
	msg string
}

func (e *riskErr) Error() string { return e.msg }
