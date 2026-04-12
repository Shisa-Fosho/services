package engine

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	platformnats "github.com/Shisa-Fosho/services/internal/platform/nats"
	"github.com/Shisa-Fosho/services/internal/platform/observability"
	"github.com/Shisa-Fosho/services/internal/trading"
)

// Config holds the dependencies the Engine needs. Everything except Now is
// required; Now defaults to time.Now if omitted and is injected for tests
// that need deterministic timestamps.
type Config struct {
	Repo     trading.Repository
	NATS     *platformnats.Client
	Provider MarketConfigProvider
	Logger   *zap.Logger
	Metrics  *observability.Metrics
	Now      func() time.Time
}

// Engine is the CLOB matching engine. It owns a map of markets to in-memory
// Books and mediates all order placement, matching, and cancellation.
//
// The engine itself is safe for concurrent use. Matching for different
// markets runs in parallel because each Book has its own mutex; within a
// market, matching is serialized through the book lock to preserve
// price-time priority.
type Engine struct {
	repo     trading.Repository
	nats     *platformnats.Client
	provider MarketConfigProvider
	logger   *zap.Logger
	metrics  *observability.Metrics
	now      func() time.Time

	mu    sync.RWMutex
	books map[string]*Book

	nextSeq atomic.Uint64
}

// New constructs an Engine. It does not load any state; callers must invoke
// Rebuild before serving traffic to restore in-memory books from the
// repository.
func New(cfg Config) *Engine {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Engine{
		repo:     cfg.Repo,
		nats:     cfg.NATS,
		provider: cfg.Provider,
		logger:   cfg.Logger,
		metrics:  cfg.Metrics,
		now:      now,
		books:    make(map[string]*Book),
	}
}

// getOrCreateBook returns the in-memory Book for the given market, lazily
// creating one if needed. Also returns the current MarketConfig so callers
// (PlaceOrder, Rebuild) can validate without a second provider round-trip.
//
// Returns an error if the provider does not know about the market.
func (e *Engine) getOrCreateBook(ctx context.Context, marketID string) (*Book, trading.MarketConfig, error) {
	cfg, err := e.provider.GetMarketConfig(ctx, marketID)
	if err != nil {
		return nil, trading.MarketConfig{}, err
	}

	// Fast path: book already exists.
	e.mu.RLock()
	book, ok := e.books[marketID]
	e.mu.RUnlock()
	if ok {
		return book, cfg, nil
	}

	// Slow path: create under the write lock, re-checking for a concurrent
	// creator to avoid overwriting a book another goroutine just inserted.
	e.mu.Lock()
	defer e.mu.Unlock()
	if book, ok = e.books[marketID]; ok {
		return book, cfg, nil
	}
	book = newBook(marketID, cfg.TokenPair)
	e.books[marketID] = book
	return book, cfg, nil
}

// book returns an existing book by market ID or nil if the market has no
// orders yet. Does not create a book. Used by Cancel and tests.
func (e *Engine) book(marketID string) *Book {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.books[marketID]
}

// PlaceOrder is the single entry point into the matching engine for new
// orders. It validates the order against market config, reserves balance
// (for BUYs), persists the order row, runs the match loop, persists any
// resulting trades, rests any GTC remainder on the book, and publishes
// NATS events for downstream consumers.
//
// Returns the persisted order (with server-generated ID populated) and
// the list of trades generated. An error is returned on validation
// failure, insufficient balance, or a FOK order that cannot fully fill.
func (e *Engine) PlaceOrder(ctx context.Context, order *trading.Order) (*trading.Order, []*trading.Trade, error) {
	// 1. Look up market config and validate the order.
	cfg, err := e.provider.GetMarketConfig(ctx, order.MarketID)
	if err != nil {
		return nil, nil, fmt.Errorf("placing order: %w", err)
	}
	if err := trading.ValidateOrder(order, cfg, e.now()); err != nil {
		return nil, nil, fmt.Errorf("placing order: %w", err)
	}

	order.Status = trading.OrderStatusOpen

	// 2. Reserve balance for BUYs. Sellers reserve tokens on-chain, which
	//    is out of scope for this PR — SELL orders skip reservation here
	//    and rely on-chain settlement to refuse delivery if the seller
	//    doesn't actually hold the tokens.
	reservedAmount := int64(0)
	if order.Side == trading.SideBuy {
		reservedAmount = order.MakerAmount
		if err := e.repo.ReserveBalance(ctx, order.Maker, reservedAmount); err != nil {
			return nil, nil, fmt.Errorf("placing order: %w", err)
		}
	}

	// 3. Persist the order row. This must happen BEFORE the order enters
	//    the in-memory book so that Rebuild can always recover state from
	//    the DB alone (see crash semantics in the plan file).
	err = e.repo.SaveOrder(ctx, order)
	// TODO(human): handle the SaveOrder result.
	//
	// There are three cases to handle here:
	//
	//   Case A — Success (err == nil):
	//     Do nothing special; fall through to the matching step below.
	//     The order row now exists in the DB with server-generated ID,
	//     CreatedAt, and UpdatedAt fields populated on `order`.
	//
	//   Case B — Duplicate (errors.Is(err, trading.ErrDuplicateOrder)):
	//     This is the idempotent-retry path. The client called PlaceOrder
	//     twice with the same signature hash — the first call succeeded
	//     and the second one hit the uniqueness constraint. We must NOT
	//     double-reserve balance. Release the reservation we just made
	//     (reservedAmount from step 2) via e.repo.ReleaseBalance, then
	//     return the existing order to the caller so the retry looks like
	//     a success. Use e.repo.GetOrder... wait, we don't have the ID.
	//     The cleanest option for v1: return nil order + nil trades + a
	//     wrapped ErrDuplicateOrder and let the REST layer (T4) handle
	//     the lookup. That keeps this function from needing a new repo
	//     method. Don't forget to release the reservation FIRST, before
	//     returning.
	//
	//   Case C — Any other error:
	//     Something went wrong (DB down, constraint violation, etc).
	//     Release the reservation so funds don't get stuck, then return
	//     a wrapped error. Ordering matters: if we return before
	//     releasing, the balance stays reserved forever.
	//
	// Feedback loop: the engine_test.go table-driven tests in the next
	// step will exercise these paths. In particular,
	// TestPlaceOrder_IdempotentDuplicate hits Case B.

	// 4. Get/create the in-memory book and run matching under its lock.
	book, _, err := e.getOrCreateBook(ctx, order.MarketID)
	if err != nil {
		return nil, nil, fmt.Errorf("placing order: %w", err)
	}

	canonical := trading.ToCanonical(order, cfg.TokenPair)
	incoming := &restingOrder{
		Order:         order,
		CanonicalSide: canonical.CanonicalSide,
		Price:         canonical.CanonicalPrice,
		RemainingSize: order.TakerAmount,
		SeqNum:        e.nextSeq.Add(1),
	}

	book.mu.Lock()

	// 5. FOK pre-check — if the order can't fully fill, reject atomically
	//    WITHOUT mutating the book.
	if order.OrderType == trading.OrderTypeFOK && !book.canFullyFill(incoming) {
		book.mu.Unlock()
		if reservedAmount > 0 {
			if relErr := e.repo.ReleaseBalance(ctx, order.Maker, reservedAmount); relErr != nil {
				e.logger.Warn("releasing balance after FOK reject",
					zap.String("order_id", order.ID),
					zap.Error(relErr))
			}
		}
		if statusErr := e.repo.UpdateOrderStatus(ctx, order.ID, trading.OrderStatusCancelled); statusErr != nil {
			e.logger.Warn("marking FOK-rejected order cancelled",
				zap.String("order_id", order.ID),
				zap.Error(statusErr))
		}
		order.Status = trading.OrderStatusCancelled
		return order, nil, fmt.Errorf("FOK order could not fully fill: %w", trading.ErrInvalidOrder)
	}

	// 6. Run the match loop. After this returns, the book has been mutated:
	//    resting makers have reduced RemainingSize (or been popped), and
	//    `incoming.RemainingSize` reflects what's left of the taker.
	fills := book.match(incoming)

	// 7. If it's a GTC order with unfilled size, rest it on the book.
	if incoming.RemainingSize > 0 && order.OrderType == trading.OrderTypeGTC {
		book.addResting(incoming)
	}

	// Snapshot best prices for the price-update event, while still holding
	// the lock to avoid racing with concurrent mutations from CancelOrder.
	var bestBidPrice, bestAskPrice int64
	if lvl := book.bestBid(); lvl != nil {
		bestBidPrice = lvl.Price
	}
	if lvl := book.bestAsk(); lvl != nil {
		bestAskPrice = lvl.Price
	}

	book.mu.Unlock()

	// 8. Persist trades + update balances + update statuses. Each SaveTrade
	//    is its own transaction so partial progress on crash is survivable
	//    (the match is durable trade-by-trade; rebuild reconstructs
	//    RemainingSize from the trades that made it).
	trades := make([]*trading.Trade, 0, len(fills))
	for _, fill := range fills {
		trade, err := e.persistFill(ctx, order, incoming, fill)
		if err != nil {
			e.logger.Error("persisting fill",
				zap.String("order_id", order.ID),
				zap.Error(err))
			break
		}
		trades = append(trades, trade)
	}

	// Update the incoming order's final status based on remaining size.
	finalStatus := trading.OrderStatusOpen
	switch {
	case incoming.RemainingSize == 0:
		finalStatus = trading.OrderStatusFilled
	case incoming.RemainingSize < order.TakerAmount:
		finalStatus = trading.OrderStatusPartiallyFilled
	}
	if finalStatus != trading.OrderStatusOpen {
		if err := e.repo.UpdateOrderStatus(ctx, order.ID, finalStatus); err != nil {
			e.logger.Warn("updating incoming order status",
				zap.String("order_id", order.ID),
				zap.Error(err))
		}
		order.Status = finalStatus
	}

	// 9. Publish NATS events outside the lock. Failures here are logged
	//    but do not roll back DB state — matches are durable in the DB.
	for _, trade := range trades {
		if err := publishMatch(ctx, e.nats, trade); err != nil {
			e.logger.Warn("publishing match event",
				zap.String("match_id", trade.MatchID),
				zap.Error(err))
		}
	}
	if len(trades) > 0 {
		lastPrice := trades[len(trades)-1].Price
		if err := publishPriceUpdate(ctx, e.nats, order.MarketID, bestBidPrice, bestAskPrice, lastPrice); err != nil {
			e.logger.Warn("publishing price update", zap.Error(err))
		}
	}

	// 10. Metrics.
	if len(trades) > 0 && e.metrics != nil {
		e.metrics.MatchesTotal.Add(float64(len(trades)))
	}

	return order, trades, nil
}

// persistFill converts a single fillResult into a persisted Trade plus the
// balance ledger updates for both sides. The maker order's status is also
// updated if the fill fully consumed it.
func (e *Engine) persistFill(ctx context.Context, takerOrder *trading.Order, incoming *restingOrder, fill fillResult) (*trading.Trade, error) {
	maker := fill.Maker

	// Cost in cents of this fill, proportional to how much of the maker's
	// total offering is being consumed. Using the maker's side determines
	// who is paying whom:
	//   maker is a SELL → maker delivers tokens, taker pays cents
	//   maker is a BUY  → maker pays cents,     taker delivers tokens
	cost := fill.FillSize * maker.Order.MakerAmount / maker.Order.TakerAmount

	var buyerAddr, sellerAddr string
	if maker.Order.Side == trading.SideSell {
		buyerAddr = takerOrder.Maker
		sellerAddr = maker.Order.Maker
	} else {
		buyerAddr = maker.Order.Maker
		sellerAddr = takerOrder.Maker
	}

	trade := &trading.Trade{
		MatchID:      uuid.NewString(),
		MakerOrderID: maker.Order.ID,
		TakerOrderID: takerOrder.ID,
		MakerAddress: maker.Order.Maker,
		TakerAddress: takerOrder.Maker,
		MarketID:     takerOrder.MarketID,
		Price:        fill.Price,
		Size:         fill.FillSize,
		MakerFee:     0, // Fee collection deferred — see follow-up issue.
		TakerFee:     0,
	}
	if err := e.repo.SaveTrade(ctx, trade); err != nil {
		return nil, fmt.Errorf("saving trade: %w", err)
	}

	// Settlement ledger: buyer's reservation becomes the seller's payout.
	if err := e.repo.DeductReserved(ctx, buyerAddr, cost); err != nil {
		e.logger.Warn("deducting reserved from buyer",
			zap.String("match_id", trade.MatchID),
			zap.String("buyer", buyerAddr),
			zap.Error(err))
	}
	if err := e.repo.CreditAvailable(ctx, sellerAddr, cost); err != nil {
		e.logger.Warn("crediting seller",
			zap.String("match_id", trade.MatchID),
			zap.String("seller", sellerAddr),
			zap.Error(err))
	}

	// Update maker order status based on whether it's fully consumed.
	makerStatus := trading.OrderStatusPartiallyFilled
	if maker.RemainingSize == 0 {
		makerStatus = trading.OrderStatusFilled
	}
	if err := e.repo.UpdateOrderStatus(ctx, maker.Order.ID, makerStatus); err != nil {
		e.logger.Warn("updating maker order status",
			zap.String("maker_order_id", maker.Order.ID),
			zap.Error(err))
	}

	return trade, nil
}
