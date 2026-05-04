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
	order       *models.Order
	createdAt   time.Time
	reservedQty float64
}

type marketBook struct {
	marketID        string
	yesBids         []*engineOrder
	yesAsks         []*engineOrder
	noBids          []*engineOrder
	noAsks          []*engineOrder
	lastTradedPrice float64
	reservations    map[string]*quoteReservation
	inbound         chan engineCommand
	quit            chan struct{}
}

type executableOrder struct {
	entry         *engineOrder
	price         float64
	complementary bool
}

type reservedSlice struct {
	entry         *engineOrder
	quantity      float64
	price         float64
	complementary bool
}

type quoteReservation struct {
	ID              string
	MarketID        string
	UserEmail       string
	Side            models.OrderSide
	Stake           float64
	AvgPrice        float64
	EstimatedShares float64
	FillsCompletely bool
	TotalCost       float64
	ExpiresAt       time.Time
	Slices          []reservedSlice
}

type commandType int

const (
	cmdSubmit commandType = iota
	cmdCancel
	cmdGetDepth
	cmdGetLastPrice
	cmdRehydrate
	cmdReserveQuote
	cmdAcceptQuote
	cmdReleaseQuote
	cmdGetQuote
	cmdGetUserOrders
)

type engineCommand struct {
	typ       commandType
	order     *models.Order
	orderID   string
	side      models.OrderSide
	bookSide  string
	levels    int
	stake     float64
	userEmail string
	ttl       time.Duration
	quoteID   string
	respCh    chan interface{}
}

type reserveQuoteResult struct {
	reservation *quoteReservation
	err         error
}

type acceptQuoteResult struct {
	reservation *quoteReservation
	err         error
}

type getQuoteResult struct {
	reservation *quoteReservation
	err         error
}

type MatchingEngine struct {
	mu    sync.RWMutex
	books map[string]*marketBook
}

var (
	engineOnce               sync.Once
	globalEngine             *MatchingEngine
	getOpenOrdersForMarketFn = db.GetOpenOrdersForMarket
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
		marketID:     marketID,
		reservations: make(map[string]*quoteReservation),
		inbound:      make(chan engineCommand, 1024),
		quit:         make(chan struct{}),
	}
	e.books[marketID] = book
	go book.run()
	go e.triggerRehydrate(marketID)
	return book
}

func (e *MatchingEngine) triggerRehydrate(marketID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	orders, err := getOpenOrdersForMarketFn(ctx, marketID)
	if err != nil {
		log.Printf("[Engine] Failed to fetch orders for rehydration %s: %v", marketID, err)
		return
	}
	book := e.getOrCreateBook(marketID)
	for i := range orders {
		book.inbound <- engineCommand{typ: cmdRehydrate, order: &orders[i]}
	}
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

func (e *MatchingEngine) ReserveQuote(marketID, userEmail string, side models.OrderSide, stake float64, ttl time.Duration) (*quoteReservation, error) {
	book := e.getOrCreateBook(marketID)
	respCh := make(chan interface{}, 1)
	book.inbound <- engineCommand{
		typ:       cmdReserveQuote,
		side:      side,
		stake:     stake,
		userEmail: userEmail,
		ttl:       ttl,
		respCh:    respCh,
	}
	resp := (<-respCh).(reserveQuoteResult)
	return resp.reservation, resp.err
}

func (e *MatchingEngine) AcceptQuote(marketID, quoteID string, order *models.Order) (*quoteReservation, error) {
	book := e.getOrCreateBook(marketID)
	respCh := make(chan interface{}, 1)
	book.inbound <- engineCommand{
		typ:     cmdAcceptQuote,
		quoteID: quoteID,
		order:   order,
		respCh:  respCh,
	}
	resp := (<-respCh).(acceptQuoteResult)
	return resp.reservation, resp.err
}

func (e *MatchingEngine) ReleaseQuote(marketID, quoteID string) {
	book := e.getOrCreateBook(marketID)
	book.inbound <- engineCommand{typ: cmdReleaseQuote, quoteID: quoteID}
}

func (e *MatchingEngine) GetQuote(marketID, quoteID string) (*quoteReservation, error) {
	book := e.getOrCreateBook(marketID)
	respCh := make(chan interface{}, 1)
	book.inbound <- engineCommand{typ: cmdGetQuote, quoteID: quoteID, respCh: respCh}
	resp := (<-respCh).(getQuoteResult)
	return resp.reservation, resp.err
}

func (e *MatchingEngine) GetUserOrders(marketID, userEmail string) []*models.Order {
	book := e.getOrCreateBook(marketID)
	respCh := make(chan interface{}, 1)
	book.inbound <- engineCommand{typ: cmdGetUserOrders, userEmail: userEmail, respCh: respCh}
	resp := <-respCh
	if orders, ok := resp.([]*models.Order); ok {
		return orders
	}
	return nil
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
	case cmdRehydrate:
		b.rehydrate(cmd.order)
	case cmdCancel:
		b.removeOrder(cmd.orderID)
	case cmdGetDepth:
		cmd.respCh <- b.depth(cmd.side, cmd.bookSide, cmd.levels)
	case cmdGetLastPrice:
		cmd.respCh <- b.lastTradedPrice
	case cmdReserveQuote:
		reservation, err := b.reserveQuote(cmd.userEmail, cmd.side, cmd.stake, cmd.ttl)
		cmd.respCh <- reserveQuoteResult{reservation: reservation, err: err}
	case cmdAcceptQuote:
		reservation, err := b.acceptQuote(cmd.quoteID, cmd.order)
		cmd.respCh <- acceptQuoteResult{reservation: reservation, err: err}
	case cmdReleaseQuote:
		b.releaseQuote(cmd.quoteID)
	case cmdGetQuote:
		reservation, err := b.getQuote(cmd.quoteID)
		cmd.respCh <- getQuoteResult{reservation: reservation, err: err}
	case cmdGetUserOrders:
		cmd.respCh <- b.getUserOrders(cmd.userEmail)
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

func (b *marketBook) rehydrate(order *models.Order) {
	if b.hasOrder(order.ID) {
		return
	}
	b.addToBook(order)
}

func (b *marketBook) executeMarketOrder(order *models.Order) {
	asks := b.executableAsks(order.Side)
	if len(asks) == 0 {
		log.Printf("[Engine] Market order %s has no liquidity — cancelling", order.ID)
		go cancelOrderAsync(order.ID, order.UserEmail)
		return
	}
	remaining := order.RemainingQty
	for len(asks) > 0 && remaining > 0 {
		best := asks[0]
		fillQty := min64(remaining, b.availableQty(best.entry))
		if fillQty <= 0 {
			asks = asks[1:]
			continue
		}
		if best.complementary {
			order.Price = best.price
			b.executeCrossFill(order, best.entry.order, fillQty)
		} else {
			b.executeFill(order, best.entry.order, fillQty, best.price)
		}
		remaining -= fillQty
		if best.entry.order.RemainingQty == 0 {
			b.removeFilledOrder(best.entry)
		}
		asks = b.executableAsks(order.Side)
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
	asks := b.executableAsks(order.Side)
	remaining := order.RemainingQty
	for len(asks) > 0 && remaining > 0 {
		best := asks[0]
		if order.Price < best.price {
			break
		}
		fillQty := min64(remaining, b.availableQty(best.entry))
		if fillQty <= 0 {
			asks = asks[1:]
			continue
		}
		if best.complementary {
			b.executeCrossFill(order, best.entry.order, fillQty)
		} else {
			b.executeFill(order, best.entry.order, fillQty, best.price)
		}
		remaining -= fillQty
		if best.entry.order.RemainingQty == 0 {
			b.removeFilledOrder(best.entry)
		}
		asks = b.executableAsks(order.Side)
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

func (b *marketBook) executeCrossFill(taker, maker *models.Order, qty float64) {
	takerPrice := taker.Price
	makerPrice := maker.Price
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
	b.lastTradedPrice = takerPrice
	log.Printf("[Engine] CrossFill: market=%s taker=%s(%s@%.2f) maker=%s(%s@%.2f) qty=%.2f",
		b.marketID, taker.ID, taker.Side, takerPrice, maker.ID, maker.Side, makerPrice, qty)
	go persistCrossFillAsync(taker, maker, qty, takerPrice, makerPrice, b.marketID)
}

func (b *marketBook) addToBook(order *models.Order) {
	if b.hasOrder(order.ID) {
		return
	}
	entry := &engineOrder{order: order, createdAt: order.CreatedAt}
	bids := b.bidsFor(order.Side)
	*bids = append(*bids, entry)
	sortEngineOrders(*bids)
}

func (b *marketBook) hasOrder(orderID string) bool {
	for _, e := range b.yesBids {
		if e.order.ID == orderID {
			return true
		}
	}
	for _, e := range b.yesAsks {
		if e.order.ID == orderID {
			return true
		}
	}
	for _, e := range b.noBids {
		if e.order.ID == orderID {
			return true
		}
	}
	for _, e := range b.noAsks {
		if e.order.ID == orderID {
			return true
		}
	}
	return false
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
	var source []OrderbookLevel
	if bookSide == "bids" {
		source = aggregateLevels(b.rawBookLevels(side, "bids"), true)
	} else {
		source = aggregateLevels(b.executableAsks(side), false)
	}
	if len(source) > maxLevels {
		source = source[:maxLevels]
	}
	return source
}

func (b *marketBook) rawBookLevels(side models.OrderSide, bookSide string) []*engineOrder {
	if side == models.OrderSideYes {
		if bookSide == "bids" {
			return b.yesBids
		}
		return b.yesAsks
	}
	if bookSide == "bids" {
		return b.noBids
	}
	return b.noAsks
}

func (b *marketBook) executableAsks(side models.OrderSide) []executableOrder {
	rawAsks := *b.asksFor(side)
	counterBids := *b.counterBids(side)
	asks := make([]executableOrder, 0, len(rawAsks)+len(counterBids))
	for _, entry := range rawAsks {
		if b.availableQty(entry) <= 0 {
			continue
		}
		asks = append(asks, executableOrder{
			entry: entry,
			price: entry.order.Price,
		})
	}
	for _, entry := range counterBids {
		if b.availableQty(entry) <= 0 {
			continue
		}
		asks = append(asks, executableOrder{
			entry:         entry,
			price:         1 - entry.order.Price,
			complementary: true,
		})
	}
	sort.Slice(asks, func(i, j int) bool {
		if asks[i].price != asks[j].price {
			return asks[i].price < asks[j].price
		}
		return asks[i].entry.createdAt.Before(asks[j].entry.createdAt)
	})
	return asks
}

func (b *marketBook) availableQty(entry *engineOrder) float64 {
	available := entry.order.RemainingQty - entry.reservedQty
	if available < 0 {
		return 0
	}
	return available
}

func (b *marketBook) counterBids(side models.OrderSide) *[]*engineOrder {
	if side == models.OrderSideYes {
		return &b.noBids
	}
	return &b.yesBids
}

func (b *marketBook) removeFilledOrder(target *engineOrder) {
	removeEntry := func(book *[]*engineOrder) bool {
		for i, entry := range *book {
			if entry == target {
				*book = append((*book)[:i], (*book)[i+1:]...)
				return true
			}
		}
		return false
	}
	if removeEntry(&b.yesBids) || removeEntry(&b.yesAsks) || removeEntry(&b.noBids) {
		return
	}
	removeEntry(&b.noAsks)
}

func aggregateLevels(source interface{}, descending bool) []OrderbookLevel {
	priceMap := make(map[float64]*OrderbookLevel)
	priceOrder := []float64{}
	switch entries := source.(type) {
	case []*engineOrder:
		for _, e := range entries {
			qty := e.order.RemainingQty
			if qty <= 0 {
				continue
			}
			p := e.order.Price
			if _, exists := priceMap[p]; !exists {
				priceMap[p] = &OrderbookLevel{Price: p}
				priceOrder = append(priceOrder, p)
			}
			priceMap[p].Quantity += qty
			priceMap[p].Orders++
		}
	case []executableOrder:
		for _, e := range entries {
			qty := e.entry.order.RemainingQty - e.entry.reservedQty
			if qty <= 0 {
				continue
			}
			p := e.price
			if _, exists := priceMap[p]; !exists {
				priceMap[p] = &OrderbookLevel{Price: p}
				priceOrder = append(priceOrder, p)
			}
			priceMap[p].Quantity += qty
			priceMap[p].Orders++
		}
	}
	if descending {
		sort.Slice(priceOrder, func(i, j int) bool { return priceOrder[i] > priceOrder[j] })
	} else {
		sort.Slice(priceOrder, func(i, j int) bool { return priceOrder[i] < priceOrder[j] })
	}
	levels := make([]OrderbookLevel, 0, len(priceOrder))
	for _, p := range priceOrder {
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

func (b *marketBook) reserveQuote(userEmail string, side models.OrderSide, stake float64, ttl time.Duration) (*quoteReservation, error) {
	if stake <= 0 {
		return nil, fmt.Errorf("stake must be positive")
	}
	asks := b.executableAsks(side)
	if len(asks) == 0 {
		return nil, fmt.Errorf("no liquidity available for %s", side)
	}
	remainingStake := stake
	totalShares := 0.0
	totalCost := 0.0
	slices := make([]reservedSlice, 0)
	for _, ask := range asks {
		if remainingStake <= 0 {
			break
		}
		availableQty := b.availableQty(ask.entry)
		if availableQty <= 0 {
			continue
		}
		maxCost := ask.price * availableQty
		reserveQty := availableQty
		reserveCost := maxCost
		if remainingStake < maxCost {
			reserveCost = remainingStake
			reserveQty = remainingStake / ask.price
		}
		ask.entry.reservedQty += reserveQty
		slices = append(slices, reservedSlice{
			entry:         ask.entry,
			quantity:      reserveQty,
			price:         ask.price,
			complementary: ask.complementary,
		})
		totalShares += reserveQty
		totalCost += reserveCost
		remainingStake -= reserveCost
	}
	if totalShares == 0 {
		return nil, fmt.Errorf("no liquidity available for %s", side)
	}
	reservation := &quoteReservation{
		ID:              fmt.Sprintf("QTE_%s", time.Now().UTC().Format("20060102150405.000000000")),
		MarketID:        b.marketID,
		UserEmail:       userEmail,
		Side:            side,
		Stake:           stake,
		AvgPrice:        totalCost / totalShares,
		EstimatedShares: totalShares,
		FillsCompletely: remainingStake <= 1e-9,
		TotalCost:       totalCost,
		ExpiresAt:       time.Now().UTC().Add(ttl),
		Slices:          slices,
	}
	b.reservations[reservation.ID] = reservation
	return reservation, nil
}

func (b *marketBook) releaseQuote(quoteID string) {
	reservation, ok := b.reservations[quoteID]
	if !ok {
		return
	}
	for _, slice := range reservation.Slices {
		slice.entry.reservedQty -= slice.quantity
		if slice.entry.reservedQty < 0 {
			slice.entry.reservedQty = 0
		}
	}
	delete(b.reservations, quoteID)
}

func (b *marketBook) acceptQuote(quoteID string, order *models.Order) (*quoteReservation, error) {
	reservation, ok := b.reservations[quoteID]
	if !ok {
		return nil, fmt.Errorf("quote not found")
	}
	if time.Now().UTC().After(reservation.ExpiresAt) {
		b.releaseQuote(quoteID)
		return nil, fmt.Errorf("quote expired")
	}
	if reservation.UserEmail != order.UserEmail {
		return nil, fmt.Errorf("quote does not belong to user")
	}
	for _, slice := range reservation.Slices {
		if slice.entry.reservedQty+1e-9 < slice.quantity || slice.entry.order.RemainingQty+1e-9 < slice.quantity {
			b.releaseQuote(quoteID)
			return nil, fmt.Errorf("reserved liquidity is no longer available")
		}
	}
	for _, slice := range reservation.Slices {
		slice.entry.reservedQty -= slice.quantity
		order.Price = slice.price
		if slice.complementary {
			b.executeCrossFill(order, slice.entry.order, slice.quantity)
		} else {
			b.executeFill(order, slice.entry.order, slice.quantity, slice.price)
		}
		if slice.entry.order.RemainingQty == 0 {
			b.removeFilledOrder(slice.entry)
		}
	}
	order.RemainingQty = max64(0, order.Quantity-order.FilledQty)
	if order.RemainingQty == 0 {
		order.Status = models.OrderStatusFilled
	} else if order.FilledQty > 0 {
		order.Status = models.OrderStatusPartiallyFilled
	}
	delete(b.reservations, quoteID)
	return reservation, nil
}

func (b *marketBook) getUserOrders(userEmail string) []*models.Order {
	var orders []*models.Order
	for _, e := range b.yesBids {
		if e.order.UserEmail == userEmail {
			orders = append(orders, e.order)
		}
	}
	for _, e := range b.noBids {
		if e.order.UserEmail == userEmail {
			orders = append(orders, e.order)
		}
	}
	return orders
}

func (b *marketBook) getQuote(quoteID string) (*quoteReservation, error) {
	reservation, ok := b.reservations[quoteID]
	if !ok {
		return nil, fmt.Errorf("quote not found")
	}
	return reservation, nil
}

func sortEngineOrders(orders []*engineOrder) {
	sort.Slice(orders, func(i, j int) bool {
		if orders[i].order.Price != orders[j].order.Price {
			return orders[i].order.Price > orders[j].order.Price
		}
		return orders[i].createdAt.Before(orders[j].createdAt)
	})
}

func persistFillAsync(taker, maker *models.Order, qty, price float64, marketID string) {
	if !persistenceReady() {
		return
	}
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
	makerStatus := models.OrderStatusPartiallyFilled
	if maker.RemainingQty == 0 {
		makerStatus = models.OrderStatusFilled
	}
	if err := db.RedisBatchUpdateFills(ctx, []db.OrderFillUpdate{
		{ID: taker.ID, FilledQty: taker.FilledQty, RemainingQty: taker.RemainingQty, Status: takerStatus},
		{ID: maker.ID, FilledQty: maker.FilledQty, RemainingQty: maker.RemainingQty, Status: makerStatus},
	}); err != nil {
		log.Printf("[Engine] Redis fill batch update failed: %v", err)
	}
	takerSnap := *taker
	takerSnap.Status = takerStatus
	db.AsyncSyncFillToPG(&takerSnap)
	makerSnap := *maker
	makerSnap.Status = makerStatus
	db.AsyncSyncFillToPG(&makerSnap)

	if err := services.DeductLockedBalanceByCurrency(ctx, taker.UserEmail, qty*price, orderBalanceCurrency(taker.IsDemo)); err != nil {
		log.Printf("[Engine] Failed to deduct locked balance for taker %s: %v", taker.UserEmail, err)
	}
	if err := services.DeductLockedBalanceByCurrency(ctx, maker.UserEmail, qty*price, orderBalanceCurrency(maker.IsDemo)); err != nil {
		log.Printf("[Engine] Failed to deduct locked balance for maker %s: %v", maker.UserEmail, err)
	}

	if _, err := UpsertPosition(ctx, UpsertPositionInput{
		UserEmail:     taker.UserEmail,
		MarketID:      marketID,
		Side:          taker.Side,
		Shares:        qty,
		FillPrice:     price,
		QuoteCurrency: market.QuoteCurrency,
		IsDemo:        taker.IsDemo,
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
		IsDemo:        maker.IsDemo,
	}); err != nil {
		log.Printf("[Engine] Failed to upsert maker position for %s: %v", maker.UserEmail, err)
	}

	BroadcastOrderbookUpdate(marketID, "FILL", map[string]interface{}{
		"price": price,
		"qty":   qty,
		"side":  taker.Side,
	})
}

func persistCrossFillAsync(taker, maker *models.Order, qty, takerPrice, makerPrice float64, marketID string) {
	if !persistenceReady() {
		return
	}
	fillID := fmt.Sprintf("%s+%s@%.2f", shortOrderID(taker.ID), shortOrderID(maker.ID), qty)
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
	makerStatus := models.OrderStatusPartiallyFilled
	if maker.RemainingQty == 0 {
		makerStatus = models.OrderStatusFilled
	}
	if err := db.RedisBatchUpdateFills(ctx, []db.OrderFillUpdate{
		{ID: taker.ID, FilledQty: taker.FilledQty, RemainingQty: taker.RemainingQty, Status: takerStatus},
		{ID: maker.ID, FilledQty: maker.FilledQty, RemainingQty: maker.RemainingQty, Status: makerStatus},
	}); err != nil {
		log.Printf("[Engine] CrossFill: Redis fill batch update failed: %v", err)
	}
	takerSnap := *taker
	takerSnap.Status = takerStatus
	takerSnap.Price = takerPrice
	db.AsyncSyncFillToPG(&takerSnap)
	makerSnap := *maker
	makerSnap.Status = makerStatus
	makerSnap.Price = makerPrice
	db.AsyncSyncFillToPG(&makerSnap)

	log.Printf("[Engine] CrossFill[%s]: deducting — taker=%s amount=%.4f maker=%s amount=%.4f",
		fillID, taker.UserEmail, qty*takerPrice, maker.UserEmail, qty*makerPrice)
	if err := services.DeductLockedBalanceByCurrency(ctx, taker.UserEmail, qty*takerPrice, orderBalanceCurrency(taker.IsDemo)); err != nil {
		log.Printf("[Engine] CrossFill[%s]: failed to deduct locked balance for taker %s: %v", fillID, taker.UserEmail, err)
	}
	if err := services.DeductLockedBalanceByCurrency(ctx, maker.UserEmail, qty*makerPrice, orderBalanceCurrency(maker.IsDemo)); err != nil {
		log.Printf("[Engine] CrossFill[%s]: failed to deduct locked balance for maker %s: %v", fillID, maker.UserEmail, err)
	}

	if _, err := UpsertPosition(ctx, UpsertPositionInput{
		UserEmail:     taker.UserEmail,
		MarketID:      marketID,
		Side:          taker.Side,
		Shares:        qty,
		FillPrice:     takerPrice,
		QuoteCurrency: market.QuoteCurrency,
		IsDemo:        taker.IsDemo,
	}); err != nil {
		log.Printf("[Engine] CrossFill[%s]: failed to upsert taker position for %s: %v", fillID, taker.UserEmail, err)
	}

	if _, err := UpsertPosition(ctx, UpsertPositionInput{
		UserEmail:     maker.UserEmail,
		MarketID:      marketID,
		Side:          maker.Side,
		Shares:        qty,
		FillPrice:     makerPrice,
		QuoteCurrency: market.QuoteCurrency,
		IsDemo:        maker.IsDemo,
	}); err != nil {
		log.Printf("[Engine] CrossFill[%s]: failed to upsert maker position for %s: %v", fillID, maker.UserEmail, err)
	}

	BroadcastOrderbookUpdate(marketID, "FILL", map[string]interface{}{
		"taker_price": takerPrice,
		"maker_price": makerPrice,
		"qty":         qty,
		"taker_side":  taker.Side,
	})
}

func persistOrderFill(order *models.Order) {
	if !persistenceReady() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := db.RedisUpdateOrderFill(ctx, order.ID, order.FilledQty, order.RemainingQty, order.Status); err != nil {
		log.Printf("[Engine] Redis order fill update failed %s: %v", order.ID, err)
	}
	db.AsyncSyncFillToPG(order)
}

func cancelOrderAsync(orderID, userEmail string) {
	if !persistenceReady() {
		return
	}
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

func max64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func shortOrderID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func persistenceReady() bool {
	return db.Pool != nil && db.RDB != nil
}
