package markets

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/models"
	"github.com/vant-xyz/backend-code/services"
)

type engineOrder struct {
	order     *models.Order
	createdAt time.Time
}

type marketBook struct {
	marketID        string
	yesBids         []*engineOrder
	yesAsks         []*engineOrder
	noBids          []*engineOrder
	noAsks          []*engineOrder
	lastTradedPrice float64
	inbound         chan engineCommand
	quit            chan struct{}
}

type commandType int

const (
	cmdSubmit commandType = iota
	cmdCancel
	cmdGetDepth
	cmdGetLastPrice
)

type engineCommand struct {
	typ      commandType
	order    *models.Order
	orderID  string
	side     models.OrderSide
	bookSide string
	levels   int
	respCh   chan interface{}
}

type MatchingEngine struct {
	mu    sync.RWMutex
	books map[string]*marketBook
}

var (
	engineOnce   sync.Once
	globalEngine *MatchingEngine
)

func GetMatchingEngine() *MatchingEngine {
	engineOnce.Do(func() {
		globalEngine = &MatchingEngine{
			books: make(map[string]*marketBook),
		}
	})
	return globalEngine
}

func (e *MatchingEngine) getOrCreateBook(marketID string) *marketBook {
	e.mu.RLock()
	book, ok := e.books[marketID]
	e.mu.RUnlock()
	if ok {
		return book
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if book, ok = e.books[marketID]; ok {
		return book
	}
	book = &marketBook{
		marketID: marketID,
		inbound:  make(chan engineCommand, 512),
		quit:     make(chan struct{}),
	}
	e.books[marketID] = book
	go book.run()
	return book
}

func (e *MatchingEngine) Submit(order *models.Order) {
	book := e.getOrCreateBook(order.MarketID)
	book.inbound <- engineCommand{typ: cmdSubmit, order: order}
}

func (e *MatchingEngine) Cancel(orderID string) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, book := range e.books {
		book.inbound <- engineCommand{typ: cmdCancel, orderID: orderID}
	}
}

func (e *MatchingEngine) GetDepth(marketID string, side models.OrderSide, bookSide string) []OrderbookLevel {
	book := e.getOrCreateBook(marketID)
	respCh := make(chan interface{}, 1)
	book.inbound <- engineCommand{typ: cmdGetDepth, side: side, bookSide: bookSide, levels: 20, respCh: respCh}
	resp := <-respCh
	if levels, ok := resp.([]OrderbookLevel); ok {
		return levels
	}
	return nil
}

func (e *MatchingEngine) GetLastTradedPrice(marketID string) float64 {
	e.mu.RLock()
	book, ok := e.books[marketID]
	e.mu.RUnlock()
	if !ok {
		return 0
	}
	respCh := make(chan interface{}, 1)
	book.inbound <- engineCommand{typ: cmdGetLastPrice, respCh: respCh}
	resp := <-respCh
	if price, ok := resp.(float64); ok {
		return price
	}
	return 0
}

func (e *MatchingEngine) CloseMarket(marketID string) {
	e.mu.Lock()
	book, ok := e.books[marketID]
	if ok {
		close(book.quit)
		delete(e.books, marketID)
	}
	e.mu.Unlock()
}

func (b *marketBook) run() {
	for {
		select {
		case cmd := <-b.inbound:
			b.handle(cmd)
		case <-b.quit:
			return
		}
	}
}

func (b *marketBook) handle(cmd engineCommand) {
	switch cmd.typ {
	case cmdSubmit:
		b.processOrder(cmd.order)
	case cmdCancel:
		b.removeOrder(cmd.orderID)
	case cmdGetDepth:
		cmd.respCh <- b.depth(cmd.side, cmd.bookSide, cmd.levels)
	case cmdGetLastPrice:
		cmd.respCh <- b.lastTradedPrice
	}
}

func (b *marketBook) processOrder(order *models.Order) {
	switch order.Type {
	case models.OrderTypeMarket:
		b.executeMarketOrder(order)
	case models.OrderTypeLimit:
		b.executeLimitOrder(order)
	}
}

func (b *marketBook) executeMarketOrder(order *models.Order) {
	asks := b.asksFor(order.Side)
	if len(*asks) == 0 {
		log.Printf("[Engine] Market order %s has no liquidity — cancelling", order.ID)
		go cancelOrderAsync(order.ID, order.UserEmail)
		return
	}
	remaining := order.RemainingQty
	for len(*asks) > 0 && remaining > 0 {
		best := (*asks)[0]
		fillQty := min64(remaining, best.order.RemainingQty)
		b.executeFill(order, best.order, fillQty, best.order.Price)
		remaining -= fillQty
		if best.order.RemainingQty == 0 {
			*asks = (*asks)[1:]
		}
	}
	order.RemainingQty = remaining
	if remaining == 0 {
		order.Status = models.OrderStatusFilled
	} else {
		order.Status = models.OrderStatusPartiallyFilled
	}
	go persistOrderFill(order)
}

func (b *marketBook) executeLimitOrder(order *models.Order) {
	asks := b.asksFor(order.Side)
	remaining := order.RemainingQty
	for len(*asks) > 0 && remaining > 0 {
		best := (*asks)[0]
		if order.Price < best.order.Price {
			break
		}
		fillQty := min64(remaining, best.order.RemainingQty)
		b.executeFill(order, best.order, fillQty, best.order.Price)
		remaining -= fillQty
		if best.order.RemainingQty == 0 {
			*asks = (*asks)[1:]
		}
	}
	order.RemainingQty = remaining
	if remaining == 0 {
		order.Status = models.OrderStatusFilled
		go persistOrderFill(order)
		return
	}
	if order.FilledQty > 0 {
		order.Status = models.OrderStatusPartiallyFilled
	}
	b.executeCrossMatch(order)
	if order.RemainingQty == 0 {
		go persistOrderFill(order)
		return
	}
	if order.FilledQty > 0 {
		order.Status = models.OrderStatusPartiallyFilled
	}
	b.addToBook(order)
	go persistOrderFill(order)
}

func (b *marketBook) executeFill(taker, maker *models.Order, qty, price float64) {
	taker.FilledQty += qty
	taker.RemainingQty -= qty
	maker.FilledQty += qty
	maker.RemainingQty -= qty
	if maker.RemainingQty == 0 {
		maker.Status = models.OrderStatusFilled
	} else {
		maker.Status = models.OrderStatusPartiallyFilled
	}
	b.lastTradedPrice = price
	log.Printf("[Engine] Fill: market=%s taker=%s maker=%s qty=%.2f price=%.2f",
		b.marketID, taker.ID, maker.ID, qty, price)
	go persistFillAsync(taker, maker, qty, price, b.marketID)
}

func (b *marketBook) executeCrossMatch(order *models.Order) {
	var counterBids *[]*engineOrder
	if order.Side == models.OrderSideYes {
		counterBids = &b.noBids
	} else {
		counterBids = &b.yesBids
	}
	i := 0
	for i < len(*counterBids) && order.RemainingQty > 0 {
		counter := (*counterBids)[i]
		if order.Price+counter.order.Price < 1.0-1e-9 {
			break
		}
		fillQty := min64(order.RemainingQty, counter.order.RemainingQty)
		b.executeCrossFill(order, counter.order, fillQty)
		if counter.order.RemainingQty == 0 {
			*counterBids = append((*counterBids)[:i], (*counterBids)[i+1:]...)
		} else {
			i++
		}
	}
}

func (b *marketBook) executeCrossFill(taker, maker *models.Order, qty float64) {
	taker.FilledQty += qty
	taker.RemainingQty -= qty
	maker.FilledQty += qty
	maker.RemainingQty -= qty
	if taker.RemainingQty == 0 {
		taker.Status = models.OrderStatusFilled
	} else {
		taker.Status = models.OrderStatusPartiallyFilled
	}
	if maker.RemainingQty == 0 {
		maker.Status = models.OrderStatusFilled
	} else {
		maker.Status = models.OrderStatusPartiallyFilled
	}
	b.lastTradedPrice = taker.Price
	log.Printf("[Engine] CrossFill: market=%s taker=%s(%s@%.2f) maker=%s(%s@%.2f) qty=%.2f",
		b.marketID, taker.ID, taker.Side, taker.Price, maker.ID, maker.Side, maker.Price, qty)
	go persistCrossFillAsync(taker, maker, qty, b.marketID)
}

func (b *marketBook) addToBook(order *models.Order) {
	entry := &engineOrder{order: order, createdAt: order.CreatedAt}
	bids := b.bidsFor(order.Side)
	*bids = append(*bids, entry)
	sort.Slice(*bids, func(i, j int) bool {
		if (*bids)[i].order.Price != (*bids)[j].order.Price {
			return (*bids)[i].order.Price > (*bids)[j].order.Price
		}
		return (*bids)[i].createdAt.Before((*bids)[j].createdAt)
	})
}

func (b *marketBook) removeOrder(orderID string) {
	removeFn := func(book *[]*engineOrder) {
		for i, e := range *book {
			if e.order.ID == orderID {
				*book = append((*book)[:i], (*book)[i+1:]...)
				return
			}
		}
	}
	removeFn(&b.yesBids)
	removeFn(&b.yesAsks)
	removeFn(&b.noBids)
	removeFn(&b.noAsks)
}

func (b *marketBook) depth(side models.OrderSide, bookSide string, maxLevels int) []OrderbookLevel {
	var source []*engineOrder
	if side == models.OrderSideYes {
		if bookSide == "bids" {
			source = b.yesBids
		} else {
			source = b.yesAsks
		}
	} else {
		if bookSide == "bids" {
			source = b.noBids
		} else {
			source = b.noAsks
		}
	}
	priceMap := make(map[float64]*OrderbookLevel)
	priceOrder := []float64{}
	for _, e := range source {
		p := e.order.Price
		if _, exists := priceMap[p]; !exists {
			priceMap[p] = &OrderbookLevel{Price: p}
			priceOrder = append(priceOrder, p)
		}
		priceMap[p].Quantity += e.order.RemainingQty
		priceMap[p].Orders++
	}
	if bookSide == "bids" {
		sort.Slice(priceOrder, func(i, j int) bool { return priceOrder[i] > priceOrder[j] })
	} else {
		sort.Slice(priceOrder, func(i, j int) bool { return priceOrder[i] < priceOrder[j] })
	}
	levels := make([]OrderbookLevel, 0, maxLevels)
	for _, p := range priceOrder {
		if len(levels) >= maxLevels {
			break
		}
		levels = append(levels, *priceMap[p])
	}
	return levels
}

func (b *marketBook) bidsFor(side models.OrderSide) *[]*engineOrder {
	if side == models.OrderSideYes {
		return &b.yesBids
	}
	return &b.noBids
}

func (b *marketBook) asksFor(side models.OrderSide) *[]*engineOrder {
	if side == models.OrderSideYes {
		return &b.yesAsks
	}
	return &b.noAsks
}

func persistFillAsync(taker, maker *models.Order, qty, price float64, marketID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	market, err := db.GetMarketByID(ctx, marketID)
	if err != nil {
		log.Printf("[Engine] Failed to fetch market %s for fill persistence: %v", marketID, err)
		return
	}

	takerStatus := models.OrderStatusPartiallyFilled
	if taker.RemainingQty == 0 {
		takerStatus = models.OrderStatusFilled
	}
	if err := db.RedisUpdateOrderFill(ctx, taker.ID, taker.FilledQty, taker.RemainingQty, takerStatus); err != nil {
		log.Printf("[Engine] Redis fill update failed for taker %s: %v", taker.ID, err)
	}
	db.AsyncSyncFillToPG(taker.ID, taker.FilledQty, taker.RemainingQty, takerStatus)

	makerStatus := models.OrderStatusPartiallyFilled
	if maker.RemainingQty == 0 {
		makerStatus = models.OrderStatusFilled
	}
	if err := db.RedisUpdateOrderFill(ctx, maker.ID, maker.FilledQty, maker.RemainingQty, makerStatus); err != nil {
		log.Printf("[Engine] Redis fill update failed for maker %s: %v", maker.ID, err)
	}
	db.AsyncSyncFillToPG(maker.ID, maker.FilledQty, maker.RemainingQty, makerStatus)

	if err := services.DeductLockedBalance(ctx, taker.UserEmail, qty*price); err != nil {
		log.Printf("[Engine] Failed to deduct locked balance for taker %s: %v", taker.UserEmail, err)
	}
	if err := services.DeductLockedBalance(ctx, maker.UserEmail, qty*price); err != nil {
		log.Printf("[Engine] Failed to deduct locked balance for maker %s: %v", maker.UserEmail, err)
	}

	if _, err := UpsertPosition(ctx, UpsertPositionInput{
		UserEmail:     taker.UserEmail,
		MarketID:      marketID,
		Side:          taker.Side,
		Shares:        qty,
		FillPrice:     price,
		QuoteCurrency: market.QuoteCurrency,
	}); err != nil {
		log.Printf("[Engine] Failed to upsert taker position for %s: %v", taker.UserEmail, err)
	}

	if _, err := UpsertPosition(ctx, UpsertPositionInput{
		UserEmail:     maker.UserEmail,
		MarketID:      marketID,
		Side:          maker.Side,
		Shares:        qty,
		FillPrice:     price,
		QuoteCurrency: market.QuoteCurrency,
	}); err != nil {
		log.Printf("[Engine] Failed to upsert maker position for %s: %v", maker.UserEmail, err)
	}

	BroadcastOrderbookUpdate(marketID, "FILL", map[string]interface{}{
		"price": price,
		"qty":   qty,
		"side":  taker.Side,
	})
}

func persistCrossFillAsync(taker, maker *models.Order, qty float64, marketID string) {
	fillID := fmt.Sprintf("%s+%s@%.2f", taker.ID[:8], maker.ID[:8], qty)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	market, err := db.GetMarketByID(ctx, marketID)
	if err != nil {
		log.Printf("[Engine] CrossFill: failed to fetch market %s: %v", marketID, err)
		return
	}

	takerStatus := models.OrderStatusPartiallyFilled
	if taker.RemainingQty == 0 {
		takerStatus = models.OrderStatusFilled
	}
	if err := db.RedisUpdateOrderFill(ctx, taker.ID, taker.FilledQty, taker.RemainingQty, takerStatus); err != nil {
		log.Printf("[Engine] CrossFill: Redis fill update failed for taker %s: %v", taker.ID, err)
	}
	if err := db.UpdateOrderFill(ctx, taker.ID, taker.FilledQty, taker.RemainingQty, takerStatus); err != nil {
		log.Printf("[Engine] CrossFill: PG fill update failed for taker %s: %v", taker.ID, err)
	}

	makerStatus := models.OrderStatusPartiallyFilled
	if maker.RemainingQty == 0 {
		makerStatus = models.OrderStatusFilled
	}
	if err := db.RedisUpdateOrderFill(ctx, maker.ID, maker.FilledQty, maker.RemainingQty, makerStatus); err != nil {
		log.Printf("[Engine] CrossFill: Redis fill update failed for maker %s: %v", maker.ID, err)
	}
	if err := db.UpdateOrderFill(ctx, maker.ID, maker.FilledQty, maker.RemainingQty, makerStatus); err != nil {
		log.Printf("[Engine] CrossFill: PG fill update failed for maker %s: %v", maker.ID, err)
	}

	log.Printf("[Engine] CrossFill[%s]: deducting — taker=%s amount=%.4f maker=%s amount=%.4f",
		fillID, taker.UserEmail, qty*taker.Price, maker.UserEmail, qty*maker.Price)
	if err := services.DeductLockedBalance(ctx, taker.UserEmail, qty*taker.Price); err != nil {
		log.Printf("[Engine] CrossFill[%s]: failed to deduct locked balance for taker %s: %v", fillID, taker.UserEmail, err)
	} else {
		log.Printf("[Engine] CrossFill[%s]: locked balance deducted for taker %s", fillID, taker.UserEmail)
	}
	if err := services.DeductLockedBalance(ctx, maker.UserEmail, qty*maker.Price); err != nil {
		log.Printf("[Engine] CrossFill[%s]: failed to deduct locked balance for maker %s: %v", fillID, maker.UserEmail, err)
	} else {
		log.Printf("[Engine] CrossFill[%s]: locked balance deducted for maker %s", fillID, maker.UserEmail)
	}

	if _, err := UpsertPosition(ctx, UpsertPositionInput{
		UserEmail:     taker.UserEmail,
		MarketID:      marketID,
		Side:          taker.Side,
		Shares:        qty,
		FillPrice:     taker.Price,
		QuoteCurrency: market.QuoteCurrency,
	}); err != nil {
		log.Printf("[Engine] CrossFill[%s]: failed to upsert taker position for %s: %v", fillID, taker.UserEmail, err)
	}

	if _, err := UpsertPosition(ctx, UpsertPositionInput{
		UserEmail:     maker.UserEmail,
		MarketID:      marketID,
		Side:          maker.Side,
		Shares:        qty,
		FillPrice:     maker.Price,
		QuoteCurrency: market.QuoteCurrency,
	}); err != nil {
		log.Printf("[Engine] CrossFill[%s]: failed to upsert maker position for %s: %v", fillID, maker.UserEmail, err)
	}

	BroadcastOrderbookUpdate(marketID, "FILL", map[string]interface{}{
		"taker_price": taker.Price,
		"maker_price": maker.Price,
		"qty":         qty,
		"taker_side":  taker.Side,
	})
}

func persistOrderFill(order *models.Order) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := db.RedisUpdateOrderFill(ctx, order.ID, order.FilledQty, order.RemainingQty, order.Status); err != nil {
		log.Printf("[Engine] Redis order fill update failed %s: %v", order.ID, err)
	}
	db.AsyncSyncFillToPG(order.ID, order.FilledQty, order.RemainingQty, order.Status)
}

func cancelOrderAsync(orderID, userEmail string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := CancelOrder(ctx, orderID, userEmail); err != nil {
		log.Printf("[Engine] Failed to cancel unfillable market order %s: %v", orderID, err)
	}
}

func min64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}