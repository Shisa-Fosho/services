package engine

import (
	"github.com/Shisa-Fosho/services/internal/trading"
)

// fillResult is one fill produced by a match — the engine turns each of
// these into a trading.Trade with a fresh match ID and fee computation.
// Price is the execution price (the resting maker's price, never the
// incoming taker's price) — standard CLOB convention where price
// improvement accrues to the taker.
type fillResult struct {
	Maker    *restingOrder
	FillSize int64
	Price    int64
}

// priceCrosses reports whether an incoming order at takerPrice can
// immediately trade against a resting level at makerPrice on the opposite
// side. For a BUY taker, crossing means the taker is willing to pay at
// least the maker's asking price. For a SELL taker, crossing means the
// taker is willing to accept at most the maker's bid price.
func priceCrosses(takerSide trading.Side, takerPrice, makerPrice int64) bool {
	if takerSide == trading.SideBuy {
		return takerPrice >= makerPrice
	}
	return takerPrice <= makerPrice
}

// oppositeBest returns the best (index-0) level on the side opposite to the
// incoming taker: asks for a BUY taker, bids for a SELL taker. Returns nil
// if that side is empty.
//
// Caller must hold b.mu.
func (b *Book) oppositeBest(takerSide trading.Side) *priceLevel {
	if takerSide == trading.SideBuy {
		return b.bestAsk()
	}
	return b.bestBid()
}

// oppositeSide returns the side enum of orders that sit on the opposite side
// of the book from the taker. A BUY taker matches against SELL resting
// orders (asks), and vice versa. Used with popFilledHead after a fill
// fully consumes a resting order.
func oppositeSide(takerSide trading.Side) trading.Side {
	if takerSide == trading.SideBuy {
		return trading.SideSell
	}
	return trading.SideBuy
}

// canFullyFill reports whether the incoming taker order could be fully
// matched against the current book state, without mutating anything.
// Used by PlaceOrder's FOK path so it can reject before the match loop
// starts mutating the book.
//
// Caller must hold b.mu.
func (b *Book) canFullyFill(incoming *restingOrder) bool {
	remaining := incoming.RemainingSize
	levels := b.bids
	if incoming.CanonicalSide == trading.SideBuy {
		levels = b.asks
	}
	for _, level := range levels {
		if !priceCrosses(incoming.CanonicalSide, incoming.Price, level.Price) {
			return false
		}
		for _, maker := range level.Orders {
			if maker.RemainingSize >= remaining {
				return true
			}
			remaining -= maker.RemainingSize
		}
	}
	return remaining == 0
}

// match runs the core matching loop against this book for the given
// incoming (taker) order. It mutates the book in place: consumes resting
// orders at best prices until either the incoming order is fully filled
// or no more crossing liquidity exists. Each fully-consumed resting order
// is popped via popFilledHead; partial fills stay at the head of their
// level with a reduced RemainingSize.
//
// The incoming order is NOT placed on the book by this function — the
// caller decides what to do with any remaining size (rest it for GTC,
// reject it for FOK).
//
// Returns one fillResult per fill. Append-only; caller owns the slice.
//
// Caller must hold b.mu.
func (b *Book) match(incoming *restingOrder) []fillResult {
	var fills []fillResult

	for {
		level := b.oppositeBest(incoming.CanonicalSide)
		if level == nil {
			break
		}
		if !priceCrosses(incoming.CanonicalSide, incoming.Price, level.Price) {
			break
		}
		if incoming.RemainingSize == 0 {
			break
		}

		maker := level.Orders[0]
		fillSize := min(incoming.RemainingSize, maker.RemainingSize)
		fills = append(fills, fillResult{
			Maker: maker, FillSize: fillSize, Price: maker.Price,
		})
		incoming.RemainingSize -= fillSize
		maker.RemainingSize -= fillSize

		if maker.RemainingSize == 0 {
			b.popFilledHead(oppositeSide(incoming.CanonicalSide))
		}
	}

	return fills
}
