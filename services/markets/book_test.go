package markets

import (
	"testing"
	"time"

	"github.com/vant-xyz/backend-code/models"
)

// makeOrder is a test helper that builds a minimal Order.
func makeOrder(id, side, typ string, price, qty float64) *models.Order {
	return &models.Order{
		ID:           id,
		UserEmail:    "test@vant.xyz",
		MarketID:     "MKT_TEST",
		Side:         models.OrderSide(side),
		Type:         models.OrderType(typ),
		Price:        price,
		Quantity:     qty,
		FilledQty:    0,
		RemainingQty: qty,
		Status:       models.OrderStatusOpen,
		CreatedAt:    time.Now(),
	}
}

func newTestBook() *marketBook {
	return &marketBook{
		marketID: "MKT_TEST",
		inbound:  make(chan engineCommand, 512),
		quit:     make(chan struct{}),
	}
}

// ── min64 ────────────────────────────────────────────────────────────────────

func TestMin64(t *testing.T) {
	cases := []struct {
		a, b, want float64
	}{
		{10, 20, 10},
		{20, 10, 10},
		{5, 5, 5},
		{0, 1, 0},
		{99.99, 100, 99.99},
	}
	for _, tc := range cases {
		got := min64(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("min64(%.2f, %.2f) = %.2f, want %.2f", tc.a, tc.b, got, tc.want)
		}
	}
}

// ── addToBook / depth sorting ─────────────────────────────────────────────────

func TestAddToBook_BidsSortedDescending(t *testing.T) {
	book := newTestBook()
	book.addToBook(makeOrder("O1", "YES", "LIMIT", 50, 100))
	book.addToBook(makeOrder("O2", "YES", "LIMIT", 60, 100))
	book.addToBook(makeOrder("O3", "YES", "LIMIT", 55, 100))

	bids := book.yesBids
	if len(bids) != 3 {
		t.Fatalf("expected 3 bids, got %d", len(bids))
	}
	if bids[0].order.Price != 60 || bids[1].order.Price != 55 || bids[2].order.Price != 50 {
		t.Errorf("bids not sorted descending: got %.0f %.0f %.0f",
			bids[0].order.Price, bids[1].order.Price, bids[2].order.Price)
	}
}

func TestAddToBook_PriceTimePriority(t *testing.T) {
	book := newTestBook()
	early := makeOrder("O1", "YES", "LIMIT", 60, 100)
	early.CreatedAt = time.Now().Add(-2 * time.Second)

	late := makeOrder("O2", "YES", "LIMIT", 60, 100)
	late.CreatedAt = time.Now()

	book.addToBook(late)
	book.addToBook(early)

	bids := book.yesBids
	if bids[0].order.ID != "O1" {
		t.Errorf("expected earlier order O1 first, got %s", bids[0].order.ID)
	}
}

func TestAddToBook_NoBids(t *testing.T) {
	book := newTestBook()
	book.addToBook(makeOrder("O1", "NO", "LIMIT", 40, 100))
	book.addToBook(makeOrder("O2", "NO", "LIMIT", 45, 100))

	if len(book.noBids) != 2 {
		t.Fatalf("expected 2 NO bids, got %d", len(book.noBids))
	}
	if len(book.yesBids) != 0 {
		t.Errorf("YES bids should be empty when adding NO orders")
	}
	if book.noBids[0].order.Price != 45 {
		t.Errorf("NO bids not sorted descending, top price = %.0f", book.noBids[0].order.Price)
	}
}

// ── removeOrder ───────────────────────────────────────────────────────────────

func TestRemoveOrder_RemovesFromYesBids(t *testing.T) {
	book := newTestBook()
	book.addToBook(makeOrder("O1", "YES", "LIMIT", 60, 100))
	book.addToBook(makeOrder("O2", "YES", "LIMIT", 55, 100))
	book.addToBook(makeOrder("O3", "YES", "LIMIT", 50, 100))

	book.removeOrder("O2")

	if len(book.yesBids) != 2 {
		t.Fatalf("expected 2 bids after removal, got %d", len(book.yesBids))
	}
	for _, e := range book.yesBids {
		if e.order.ID == "O2" {
			t.Error("O2 still present after removeOrder")
		}
	}
}

func TestRemoveOrder_NonExistentOrderIsNoop(t *testing.T) {
	book := newTestBook()
	book.addToBook(makeOrder("O1", "YES", "LIMIT", 60, 100))

	book.removeOrder("GHOST")

	if len(book.yesBids) != 1 {
		t.Errorf("book size changed unexpectedly after removing non-existent order")
	}
}

func TestRemoveOrder_RemovesFromYesAsks(t *testing.T) {
	book := newTestBook()
	ask := &engineOrder{order: makeOrder("A1", "YES", "LIMIT", 62, 100), createdAt: time.Now()}
	book.yesAsks = append(book.yesAsks, ask)

	book.removeOrder("A1")

	if len(book.yesAsks) != 0 {
		t.Error("A1 still present in yesAsks after removeOrder")
	}
}

// ── depth ─────────────────────────────────────────────────────────────────────

func TestDepth_AggregatesSamePriceLevels(t *testing.T) {
	book := newTestBook()
	book.addToBook(makeOrder("O1", "YES", "LIMIT", 60, 100))
	book.addToBook(makeOrder("O2", "YES", "LIMIT", 60, 150))
	book.addToBook(makeOrder("O3", "YES", "LIMIT", 55, 200))

	levels := book.depth(models.OrderSideYes, "bids", 10)

	if len(levels) != 2 {
		t.Fatalf("expected 2 price levels, got %d", len(levels))
	}
	if levels[0].Price != 60 {
		t.Errorf("top level price = %.0f, want 60", levels[0].Price)
	}
	if levels[0].Quantity != 250 {
		t.Errorf("top level qty = %.0f, want 250 (100+150)", levels[0].Quantity)
	}
	if levels[0].Orders != 2 {
		t.Errorf("top level order count = %d, want 2", levels[0].Orders)
	}
	if levels[1].Price != 55 || levels[1].Quantity != 200 {
		t.Errorf("second level = price %.0f qty %.0f, want price 55 qty 200",
			levels[1].Price, levels[1].Quantity)
	}
}

func TestDepth_AsksSortedAscending(t *testing.T) {
	book := newTestBook()
	book.yesAsks = []*engineOrder{
		{order: makeOrder("A1", "YES", "LIMIT", 65, 100), createdAt: time.Now()},
		{order: makeOrder("A2", "YES", "LIMIT", 62, 100), createdAt: time.Now()},
		{order: makeOrder("A3", "YES", "LIMIT", 68, 100), createdAt: time.Now()},
	}

	levels := book.depth(models.OrderSideYes, "asks", 10)

	if len(levels) != 3 {
		t.Fatalf("expected 3 ask levels, got %d", len(levels))
	}
	if levels[0].Price != 62 || levels[1].Price != 65 || levels[2].Price != 68 {
		t.Errorf("asks not sorted ascending: %.0f %.0f %.0f",
			levels[0].Price, levels[1].Price, levels[2].Price)
	}
}

func TestDepth_RespectsMaxLevels(t *testing.T) {
	book := newTestBook()
	for i := 0; i < 15; i++ {
		book.addToBook(makeOrder(
			"O"+string(rune('A'+i)),
			"YES", "LIMIT",
			float64(50+i), 100,
		))
	}

	levels := book.depth(models.OrderSideYes, "bids", 5)
	if len(levels) != 5 {
		t.Errorf("expected 5 levels with maxLevels=5, got %d", len(levels))
	}
}

func TestDepth_EmptyBook(t *testing.T) {
	book := newTestBook()
	levels := book.depth(models.OrderSideYes, "bids", 10)
	if len(levels) != 0 {
		t.Errorf("expected empty levels for empty book, got %d", len(levels))
	}
}

// ── fill math (pure, no DB) ───────────────────────────────────────────────────
//
// We test the fill algorithm in isolation by setting up book state directly
// and running the math without the goroutine-based DB persistence.

func simulateLimitFill(book *marketBook, order *models.Order) {
	asks := book.asksFor(order.Side)
	remaining := order.RemainingQty
	for len(*asks) > 0 && remaining > 0 {
		best := (*asks)[0]
		if order.Price < best.order.Price {
			break
		}
		fillQty := min64(remaining, best.order.RemainingQty)
		order.FilledQty += fillQty
		order.RemainingQty -= fillQty
		best.order.FilledQty += fillQty
		best.order.RemainingQty -= fillQty
		if best.order.RemainingQty == 0 {
			best.order.Status = models.OrderStatusFilled
			*asks = (*asks)[1:]
		} else {
			best.order.Status = models.OrderStatusPartiallyFilled
		}
		book.lastTradedPrice = best.order.Price
		remaining -= fillQty
	}
	order.RemainingQty = remaining
	if remaining == 0 {
		order.Status = models.OrderStatusFilled
	} else if order.FilledQty > 0 {
		order.Status = models.OrderStatusPartiallyFilled
	}
}

func simulateMarketFill(book *marketBook, order *models.Order) {
	asks := book.asksFor(order.Side)
	remaining := order.RemainingQty
	for len(*asks) > 0 && remaining > 0 {
		best := (*asks)[0]
		fillQty := min64(remaining, best.order.RemainingQty)
		order.FilledQty += fillQty
		order.RemainingQty -= fillQty
		best.order.FilledQty += fillQty
		best.order.RemainingQty -= fillQty
		if best.order.RemainingQty == 0 {
			best.order.Status = models.OrderStatusFilled
			*asks = (*asks)[1:]
		} else {
			best.order.Status = models.OrderStatusPartiallyFilled
		}
		book.lastTradedPrice = best.order.Price
		remaining -= fillQty
	}
	order.RemainingQty = remaining
	if remaining == 0 {
		order.Status = models.OrderStatusFilled
	} else {
		order.Status = models.OrderStatusPartiallyFilled
	}
}

func TestFillMath_FullLimitMatch(t *testing.T) {
	book := newTestBook()
	ask := &engineOrder{order: makeOrder("ASK1", "YES", "LIMIT", 60, 100), createdAt: time.Now()}
	book.yesAsks = []*engineOrder{ask}

	taker := makeOrder("BID1", "YES", "LIMIT", 62, 100)
	simulateLimitFill(book, taker)

	if taker.Status != models.OrderStatusFilled {
		t.Errorf("taker status = %s, want FILLED", taker.Status)
	}
	if taker.FilledQty != 100 {
		t.Errorf("taker FilledQty = %.0f, want 100", taker.FilledQty)
	}
	if taker.RemainingQty != 0 {
		t.Errorf("taker RemainingQty = %.0f, want 0", taker.RemainingQty)
	}
	if ask.order.Status != models.OrderStatusFilled {
		t.Errorf("maker status = %s, want FILLED", ask.order.Status)
	}
	if book.lastTradedPrice != 60 {
		t.Errorf("lastTradedPrice = %.2f, want 60 (maker's price)", book.lastTradedPrice)
	}
	if len(book.yesAsks) != 0 {
		t.Errorf("ask should be removed from book after full fill")
	}
}

func TestFillMath_PartialFill_TakerLarger(t *testing.T) {
	book := newTestBook()
	ask := &engineOrder{order: makeOrder("ASK1", "YES", "LIMIT", 60, 100), createdAt: time.Now()}
	book.yesAsks = []*engineOrder{ask}

	taker := makeOrder("BID1", "YES", "LIMIT", 62, 150)
	simulateLimitFill(book, taker)

	if taker.Status != models.OrderStatusPartiallyFilled {
		t.Errorf("taker status = %s, want PARTIALLY_FILLED", taker.Status)
	}
	if taker.FilledQty != 100 {
		t.Errorf("taker FilledQty = %.0f, want 100", taker.FilledQty)
	}
	if taker.RemainingQty != 50 {
		t.Errorf("taker RemainingQty = %.0f, want 50", taker.RemainingQty)
	}
	if ask.order.Status != models.OrderStatusFilled {
		t.Errorf("ask status = %s, want FILLED", ask.order.Status)
	}
}

func TestFillMath_PartialFill_MakerLarger(t *testing.T) {
	book := newTestBook()
	ask := &engineOrder{order: makeOrder("ASK1", "YES", "LIMIT", 60, 200), createdAt: time.Now()}
	book.yesAsks = []*engineOrder{ask}

	taker := makeOrder("BID1", "YES", "LIMIT", 62, 100)
	simulateLimitFill(book, taker)

	if taker.Status != models.OrderStatusFilled {
		t.Errorf("taker status = %s, want FILLED", taker.Status)
	}
	if ask.order.Status != models.OrderStatusPartiallyFilled {
		t.Errorf("ask status = %s, want PARTIALLY_FILLED", ask.order.Status)
	}
	if ask.order.RemainingQty != 100 {
		t.Errorf("ask RemainingQty = %.0f, want 100", ask.order.RemainingQty)
	}
	if len(book.yesAsks) != 1 {
		t.Errorf("ask should still be in book (partially filled)")
	}
}

func TestFillMath_LimitOrderPriceTooLow_NoMatch(t *testing.T) {
	book := newTestBook()
	ask := &engineOrder{order: makeOrder("ASK1", "YES", "LIMIT", 65, 100), createdAt: time.Now()}
	book.yesAsks = []*engineOrder{ask}

	taker := makeOrder("BID1", "YES", "LIMIT", 60, 100)
	simulateLimitFill(book, taker)

	if taker.FilledQty != 0 {
		t.Errorf("taker should have 0 fills when bid < ask price, got %.0f", taker.FilledQty)
	}
	if taker.Status != models.OrderStatusOpen {
		t.Errorf("taker status = %s, want OPEN (goes to book)", taker.Status)
	}
	if len(book.yesAsks) != 1 {
		t.Error("ask should remain in book untouched")
	}
}

func TestFillMath_MarketOrder_EatsMultipleLevels(t *testing.T) {
	book := newTestBook()
	book.yesAsks = []*engineOrder{
		{order: makeOrder("A1", "YES", "LIMIT", 62, 100), createdAt: time.Now()},
		{order: makeOrder("A2", "YES", "LIMIT", 63, 100), createdAt: time.Now()},
		{order: makeOrder("A3", "YES", "LIMIT", 64, 100), createdAt: time.Now()},
	}

	taker := makeOrder("M1", "YES", "MARKET", 0, 250)
	simulateMarketFill(book, taker)

	if taker.FilledQty != 250 {
		t.Errorf("market order FilledQty = %.0f, want 250", taker.FilledQty)
	}
	if taker.Status != models.OrderStatusFilled {
		t.Errorf("market order status = %s, want FILLED", taker.Status)
	}
	if len(book.yesAsks) != 1 {
		t.Errorf("expected 1 ask remaining (A3 50 left), got %d", len(book.yesAsks))
	}
	if book.lastTradedPrice != 64 {
		t.Errorf("lastTradedPrice = %.0f, want 64", book.lastTradedPrice)
	}
}

func TestFillMath_MarketOrder_NoLiquidity(t *testing.T) {
	book := newTestBook()
	taker := makeOrder("M1", "YES", "MARKET", 0, 100)
	simulateMarketFill(book, taker)

	if taker.FilledQty != 0 {
		t.Errorf("market order against empty book should have 0 fills, got %.0f", taker.FilledQty)
	}
	if taker.Status != models.OrderStatusPartiallyFilled {
		t.Errorf("market order no-liquidity status = %s, want PARTIALLY_FILLED", taker.Status)
	}
}

func TestFillMath_FillPriceIsAlwaysMakerPrice(t *testing.T) {
	book := newTestBook()
	ask := &engineOrder{order: makeOrder("ASK1", "YES", "LIMIT", 58, 100), createdAt: time.Now()}
	book.yesAsks = []*engineOrder{ask}

	taker := makeOrder("BID1", "YES", "LIMIT", 75, 100)
	simulateLimitFill(book, taker)

	if book.lastTradedPrice != 58 {
		t.Errorf("fill should execute at maker price 58, got %.2f", book.lastTradedPrice)
	}
}

// simulateLimitOrder mirrors the full executeLimitOrder flow:
// same-side ask matching → cross-side matching → rest in book.
// No goroutines, no DB.
func simulateLimitOrder(book *marketBook, order *models.Order) {
	simulateLimitFill(book, order)
	if order.RemainingQty == 0 {
		return
	}
	simulateCrossMatch(book, order)
	if order.RemainingQty == 0 {
		return
	}
	book.addToBook(order)
}

// ── executeLimitOrder integration ────────────────────────────────────────────

func TestLimitOrder_NoCounterParty_RestsInBook(t *testing.T) {
	book := newTestBook()
	order := makeOrder("YES1", "YES", "LIMIT", 0.65, 50)
	simulateLimitOrder(book, order)

	if len(book.yesBids) != 1 {
		t.Fatalf("expected order in yesBids, got %d entries", len(book.yesBids))
	}
	if order.FilledQty != 0 {
		t.Errorf("FilledQty = %.0f, want 0", order.FilledQty)
	}
	if order.Status != models.OrderStatusOpen {
		t.Errorf("status = %s, want OPEN", order.Status)
	}
}

func TestLimitOrder_CrossMatchFills_DoesNotAddToBook(t *testing.T) {
	book := newTestBook()
	book.addToBook(makeOrder("NO1", "NO", "LIMIT", 0.35, 50))

	yes := makeOrder("YES1", "YES", "LIMIT", 0.65, 50)
	simulateLimitOrder(book, yes)

	if yes.Status != models.OrderStatusFilled {
		t.Errorf("status = %s, want FILLED", yes.Status)
	}
	if len(book.yesBids) != 0 {
		t.Errorf("filled order should not rest in book, got %d yesBids", len(book.yesBids))
	}
	if len(book.noBids) != 0 {
		t.Errorf("matched NO bid should be gone, got %d noBids", len(book.noBids))
	}
}

func TestLimitOrder_PartialCrossMatch_RestsRemainder(t *testing.T) {
	book := newTestBook()
	book.addToBook(makeOrder("NO1", "NO", "LIMIT", 0.35, 30))

	yes := makeOrder("YES1", "YES", "LIMIT", 0.65, 50)
	simulateLimitOrder(book, yes)

	// 30 cross-filled, 20 rests in yesBids
	if yes.FilledQty != 30 {
		t.Errorf("FilledQty = %.0f, want 30", yes.FilledQty)
	}
	if yes.RemainingQty != 20 {
		t.Errorf("RemainingQty = %.0f, want 20", yes.RemainingQty)
	}
	if yes.Status != models.OrderStatusPartiallyFilled {
		t.Errorf("status = %s, want PARTIALLY_FILLED", yes.Status)
	}
	if len(book.yesBids) != 1 {
		t.Errorf("remainder should rest in yesBids, got %d entries", len(book.yesBids))
	}
	if book.yesBids[0].order.RemainingQty != 20 {
		t.Errorf("resting qty = %.0f, want 20", book.yesBids[0].order.RemainingQty)
	}
}

func TestLimitOrder_SameSideAskFillsThenCrossMatchNotNeeded(t *testing.T) {
	book := newTestBook()
	// Inject a same-side ask directly (unusual but valid path)
	book.yesAsks = []*engineOrder{
		{order: makeOrder("ASK1", "YES", "LIMIT", 0.60, 50), createdAt: makeOrder("ASK1", "YES", "LIMIT", 0.60, 50).CreatedAt},
	}

	yes := makeOrder("YES1", "YES", "LIMIT", 0.65, 50)
	simulateLimitOrder(book, yes)

	if yes.Status != models.OrderStatusFilled {
		t.Errorf("status = %s, want FILLED (filled via same-side ask)", yes.Status)
	}
	if len(book.yesBids) != 0 {
		t.Errorf("fully filled order should not rest in book")
	}
}

func TestLimitOrder_PriceIncompatible_RestsWithoutFill(t *testing.T) {
	book := newTestBook()
	// NO bid at 0.30 — YES at 0.60 sums to 0.90, no match
	book.addToBook(makeOrder("NO1", "NO", "LIMIT", 0.30, 50))

	yes := makeOrder("YES1", "YES", "LIMIT", 0.60, 50)
	simulateLimitOrder(book, yes)

	if yes.FilledQty != 0 {
		t.Errorf("FilledQty = %.0f, want 0", yes.FilledQty)
	}
	if len(book.yesBids) != 1 {
		t.Errorf("order should rest in yesBids, got %d", len(book.yesBids))
	}
	if len(book.noBids) != 1 {
		t.Errorf("unmatched NO bid should remain, got %d", len(book.noBids))
	}
}

// ── cross-side matching ───────────────────────────────────────────────────────
//
// simulateCrossMatch mirrors executeCrossMatch/executeCrossFill without the
// DB persistence goroutine so tests stay pure.

func simulateCrossMatch(book *marketBook, order *models.Order) {
	var counterBids *[]*engineOrder
	if order.Side == models.OrderSideYes {
		counterBids = &book.noBids
	} else {
		counterBids = &book.yesBids
	}
	i := 0
	for i < len(*counterBids) && order.RemainingQty > 0 {
		counter := (*counterBids)[i]
		if order.Price+counter.order.Price < 1.0-1e-9 {
			break
		}
		fillQty := min64(order.RemainingQty, counter.order.RemainingQty)
		order.FilledQty += fillQty
		order.RemainingQty -= fillQty
		counter.order.FilledQty += fillQty
		counter.order.RemainingQty -= fillQty
		if counter.order.RemainingQty == 0 {
			counter.order.Status = models.OrderStatusFilled
			*counterBids = append((*counterBids)[:i], (*counterBids)[i+1:]...)
		} else {
			counter.order.Status = models.OrderStatusPartiallyFilled
			i++
		}
		book.lastTradedPrice = order.Price
	}
	if order.RemainingQty == 0 {
		order.Status = models.OrderStatusFilled
	} else if order.FilledQty > 0 {
		order.Status = models.OrderStatusPartiallyFilled
	}
}

func TestCrossMatch_ExactComplement_FullFill(t *testing.T) {
	book := newTestBook()
	noOrder := makeOrder("NO1", "NO", "LIMIT", 0.35, 50)
	book.addToBook(noOrder)

	yesTaker := makeOrder("YES1", "YES", "LIMIT", 0.65, 50)
	simulateCrossMatch(book, yesTaker)

	if yesTaker.Status != models.OrderStatusFilled {
		t.Errorf("YES taker status = %s, want FILLED", yesTaker.Status)
	}
	if yesTaker.FilledQty != 50 {
		t.Errorf("YES taker FilledQty = %.0f, want 50", yesTaker.FilledQty)
	}
	if noOrder.Status != models.OrderStatusFilled {
		t.Errorf("NO maker status = %s, want FILLED", noOrder.Status)
	}
	if len(book.noBids) != 0 {
		t.Error("NO bid should be removed after full fill")
	}
}

func TestCrossMatch_PriceSumBelowOne_NoFill(t *testing.T) {
	book := newTestBook()
	noOrder := makeOrder("NO1", "NO", "LIMIT", 0.30, 50)
	book.addToBook(noOrder)

	yesTaker := makeOrder("YES1", "YES", "LIMIT", 0.60, 50)
	simulateCrossMatch(book, yesTaker)

	if yesTaker.FilledQty != 0 {
		t.Errorf("should have 0 fills when prices sum to 0.90, got %.2f", yesTaker.FilledQty)
	}
	if yesTaker.Status != models.OrderStatusOpen {
		t.Errorf("status = %s, want OPEN", yesTaker.Status)
	}
	if len(book.noBids) != 1 {
		t.Error("NO bid should remain untouched")
	}
}

func TestCrossMatch_PriceSumAboveOne_Fills(t *testing.T) {
	book := newTestBook()
	noOrder := makeOrder("NO1", "NO", "LIMIT", 0.40, 50)
	book.addToBook(noOrder)

	yesTaker := makeOrder("YES1", "YES", "LIMIT", 0.70, 50)
	simulateCrossMatch(book, yesTaker)

	if yesTaker.Status != models.OrderStatusFilled {
		t.Errorf("YES taker status = %s, want FILLED (sum=1.10)", yesTaker.Status)
	}
	if yesTaker.FilledQty != 50 {
		t.Errorf("FilledQty = %.0f, want 50", yesTaker.FilledQty)
	}
}

func TestCrossMatch_PartialFill_TakerLarger(t *testing.T) {
	book := newTestBook()
	noOrder := makeOrder("NO1", "NO", "LIMIT", 0.35, 100)
	book.addToBook(noOrder)

	yesTaker := makeOrder("YES1", "YES", "LIMIT", 0.65, 150)
	simulateCrossMatch(book, yesTaker)

	if yesTaker.FilledQty != 100 {
		t.Errorf("taker FilledQty = %.0f, want 100", yesTaker.FilledQty)
	}
	if yesTaker.RemainingQty != 50 {
		t.Errorf("taker RemainingQty = %.0f, want 50", yesTaker.RemainingQty)
	}
	if yesTaker.Status != models.OrderStatusPartiallyFilled {
		t.Errorf("taker status = %s, want PARTIALLY_FILLED", yesTaker.Status)
	}
	if noOrder.Status != models.OrderStatusFilled {
		t.Errorf("maker status = %s, want FILLED", noOrder.Status)
	}
	if len(book.noBids) != 0 {
		t.Error("fully filled NO bid should be removed from book")
	}
}

func TestCrossMatch_PartialFill_MakerLarger(t *testing.T) {
	book := newTestBook()
	noOrder := makeOrder("NO1", "NO", "LIMIT", 0.35, 200)
	book.addToBook(noOrder)

	yesTaker := makeOrder("YES1", "YES", "LIMIT", 0.65, 100)
	simulateCrossMatch(book, yesTaker)

	if yesTaker.Status != models.OrderStatusFilled {
		t.Errorf("taker status = %s, want FILLED", yesTaker.Status)
	}
	if noOrder.Status != models.OrderStatusPartiallyFilled {
		t.Errorf("maker status = %s, want PARTIALLY_FILLED", noOrder.Status)
	}
	if noOrder.RemainingQty != 100 {
		t.Errorf("maker RemainingQty = %.0f, want 100", noOrder.RemainingQty)
	}
	if len(book.noBids) != 1 {
		t.Error("partially filled NO bid should remain in book")
	}
}

func TestCrossMatch_EachSidePaysOwnPrice(t *testing.T) {
	// lastTradedPrice is set to taker's price (not maker's), confirming
	// that taker and maker each pay their own locked amount.
	book := newTestBook()
	noOrder := makeOrder("NO1", "NO", "LIMIT", 0.40, 50)
	book.addToBook(noOrder)

	yesTaker := makeOrder("YES1", "YES", "LIMIT", 0.70, 50)
	simulateCrossMatch(book, yesTaker)

	if book.lastTradedPrice != 0.70 {
		t.Errorf("lastTradedPrice = %.2f, want 0.70 (taker's price)", book.lastTradedPrice)
	}
}

func TestCrossMatch_MultipleCounterBids_OnlyMatchingOnes(t *testing.T) {
	book := newTestBook()
	book.addToBook(makeOrder("NO1", "NO", "LIMIT", 0.35, 50))
	book.addToBook(makeOrder("NO2", "NO", "LIMIT", 0.30, 50))
	book.addToBook(makeOrder("NO3", "NO", "LIMIT", 0.20, 50))

	yesTaker := makeOrder("YES1", "YES", "LIMIT", 0.65, 200)
	simulateCrossMatch(book, yesTaker)

	// Only NO1 (0.35) sums to 1.00 — NO2 (0.30) and NO3 (0.20) do not match
	if yesTaker.FilledQty != 50 {
		t.Errorf("FilledQty = %.0f, want 50 (only one counter bid qualifies)", yesTaker.FilledQty)
	}
	if len(book.noBids) != 2 {
		t.Errorf("noBids len = %d, want 2 (NO2 and NO3 should remain)", len(book.noBids))
	}
}

func TestCrossMatch_NoOrderMatchesYesBids(t *testing.T) {
	book := newTestBook()
	yesOrder := makeOrder("YES1", "YES", "LIMIT", 0.65, 50)
	book.addToBook(yesOrder)

	noTaker := makeOrder("NO1", "NO", "LIMIT", 0.35, 50)
	simulateCrossMatch(book, noTaker)

	if noTaker.Status != models.OrderStatusFilled {
		t.Errorf("NO taker status = %s, want FILLED", noTaker.Status)
	}
	if yesOrder.Status != models.OrderStatusFilled {
		t.Errorf("YES maker status = %s, want FILLED", yesOrder.Status)
	}
	if len(book.yesBids) != 0 {
		t.Error("YES bid should be removed after full fill")
	}
}

func TestCrossMatch_LastTradedPriceUpdates(t *testing.T) {
	book := newTestBook()
	book.addToBook(makeOrder("NO1", "NO", "LIMIT", 0.35, 50))
	book.addToBook(makeOrder("NO2", "NO", "LIMIT", 0.38, 50))

	// NO2 (0.38) is best bid, YES at 0.65 matches both (0.65+0.38=1.03, 0.65+0.35=1.00)
	yesTaker := makeOrder("YES1", "YES", "LIMIT", 0.65, 100)
	simulateCrossMatch(book, yesTaker)

	if yesTaker.Status != models.OrderStatusFilled {
		t.Errorf("taker status = %s, want FILLED", yesTaker.Status)
	}
	if book.lastTradedPrice != 0.65 {
		t.Errorf("lastTradedPrice = %.2f, want 0.65 (taker's price)", book.lastTradedPrice)
	}
}

// ── lastTradedPrice ───────────────────────────────────────────────────────────

func TestLastTradedPrice_UpdatesOnEachFill(t *testing.T) {
	book := newTestBook()
	book.yesAsks = []*engineOrder{
		{order: makeOrder("A1", "YES", "LIMIT", 62, 50), createdAt: time.Now()},
		{order: makeOrder("A2", "YES", "LIMIT", 64, 50), createdAt: time.Now()},
	}

	taker := makeOrder("M1", "YES", "MARKET", 0, 100)
	simulateMarketFill(book, taker)

	if book.lastTradedPrice != 64 {
		t.Errorf("lastTradedPrice after eating two levels = %.0f, want 64 (last fill level)", book.lastTradedPrice)
	}
}
