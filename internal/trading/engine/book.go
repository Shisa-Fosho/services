package engine

import (
	"slices"
	"sync"

	"github.com/Shisa-Fosho/services/internal/trading"
)

// restingOrder is an order sitting on the book, stored in canonical form.
// RemainingSize tracks unfilled size in taker units (contracts).
type restingOrder struct {
	Order         *trading.Order // the original unconverted order
	CanonicalSide trading.Side   // side after canonical conversion (BUY or SELL on the YES token)
	Price         int64          // canonical price, 1..99 cents
	RemainingSize int64          // unfilled size, in taker units
	SeqNum        uint64         // monotonic counter assigned at insertion, for FIFO time priority
}

// priceLevel is a FIFO queue of resting orders at a single price point.
// Orders[0] is the oldest (lowest SeqNum) and is the only order that can
// match at this level — price-time priority is enforced implicitly by
// only ever consuming from the head.
type priceLevel struct {
	Price  int64
	Orders []*restingOrder
}

// Book stores bids and asks for a single market in canonical (YES-side) form.
//
// bids is sorted DESCENDING by Price — index 0 is the best bid (highest price).
// asks is sorted ASCENDING  by Price — index 0 is the best ask (lowest price).
// Within a priceLevel, orders are FIFO by SeqNum.
//
// byID maps order IDs to their resting pointer so cancellations are O(1).
//
// Book is safe for concurrent use: every mutation holds mu.
type Book struct {
	mu       sync.Mutex
	marketID string
	pair     trading.TokenPair
	bids     []*priceLevel
	asks     []*priceLevel
	byID     map[string]*restingOrder
}

// newBook returns an empty Book for the given market and token pair.
func newBook(marketID string, pair trading.TokenPair) *Book {
	return &Book{
		marketID: marketID,
		pair:     pair,
		byID:     make(map[string]*restingOrder),
	}
}

// bestBid returns the highest-priced bid level, or nil if there are no bids.
// Caller must hold b.mu.
func (b *Book) bestBid() *priceLevel {
	if len(b.bids) == 0 {
		return nil
	}
	return b.bids[0]
}

// bestAsk returns the lowest-priced ask level, or nil if there are no asks.
// Caller must hold b.mu.
func (b *Book) bestAsk() *priceLevel {
	if len(b.asks) == 0 {
		return nil
	}
	return b.asks[0]
}

// addResting places a new resting order on the correct side of the book at
// the correct price level. If a priceLevel for ro.Price already exists, the
// order is appended to its FIFO queue. Otherwise a new priceLevel is inserted
// so that bids remain sorted DESCENDING and asks remain sorted ASCENDING by
// Price. The order is also registered in b.byID for O(1) lookup by ID.
//
// Caller must hold b.mu.
func (book *Book) addResting(ro *restingOrder) {

	book.byID[ro.Order.ID] = ro
	if ro.CanonicalSide == trading.SideBuy {
		level, idx, found := findLevel(book.bids, ro.Price, ro.CanonicalSide)
		if found {
			level.Orders = append(level.Orders, ro)
			return
		}
		level = &priceLevel{
			Price:  ro.Price,
			Orders: []*restingOrder{ro},
		}
		book.bids = slices.Insert(book.bids, idx, level)

	} else {
		level, idx, found := findLevel(book.asks, ro.Price, ro.CanonicalSide)
		if found {
			level.Orders = append(level.Orders, ro)
			return
		}
		level = &priceLevel{
			Price:  ro.Price,
			Orders: []*restingOrder{ro},
		}
		book.asks = slices.Insert(book.asks, idx, level)

	}

}

func findLevel(levels []*priceLevel, price int64, side trading.Side) (level *priceLevel, idx int, found bool) {
	for i, level := range levels {
		if level.Price == price {
			return level, i, true
		}

		if side == trading.SideBuy {
			if price > level.Price {
				return nil, i, false
			}
		}
		if side == trading.SideSell {
			if price < level.Price {
				return nil, i, false
			}
		}

	}

	return nil, len(levels), false
}

// removeOrder removes an order from the book by ID. If the order is the
// last one at its price level, the level itself is also removed so that
// bestBid/bestAsk stay accurate. No-op if the order is not on the book.
//
// Caller must hold b.mu.
func (b *Book) removeOrder(orderID string) {
	ro, ok := b.byID[orderID]
	if !ok {
		return
	}
	delete(b.byID, orderID)

	side := &b.bids
	if ro.CanonicalSide == trading.SideSell {
		side = &b.asks
	}

	for i, level := range *side {
		if level.Price != ro.Price {
			continue
		}
		for j, o := range level.Orders {
			if o.Order.ID != orderID {
				continue
			}
			level.Orders = append(level.Orders[:j], level.Orders[j+1:]...)
			if len(level.Orders) == 0 {
				*side = append((*side)[:i], (*side)[i+1:]...)
			}
			return
		}
	}
}

// popFilledHead removes the head order of the given side's best level when
// that order has been fully consumed by a match. If the level becomes empty,
// the level itself is removed from the side slice.
//
// Caller must hold b.mu.
func (b *Book) popFilledHead(side trading.Side) {
	sideSlice := &b.bids
	if side == trading.SideSell {
		sideSlice = &b.asks
	}
	if len(*sideSlice) == 0 {
		return
	}
	level := (*sideSlice)[0]
	if len(level.Orders) == 0 {
		*sideSlice = (*sideSlice)[1:]
		return
	}
	head := level.Orders[0]
	delete(b.byID, head.Order.ID)
	level.Orders = level.Orders[1:]
	if len(level.Orders) == 0 {
		*sideSlice = (*sideSlice)[1:]
	}
}
