package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"

	platformnats "github.com/Shisa-Fosho/services/internal/platform/nats"
	"github.com/Shisa-Fosho/services/internal/trading"
	tradingv1 "github.com/Shisa-Fosho/services/proto/gen/trading/v1"
)

// Subject and stream names published to by the engine. Subjects follow the
// {domain}.{action} convention documented in docs/conventions.md.
const (
	// StreamNameMatch is the JetStream stream that durably stores match
	// events for the settlement worker.
	StreamNameMatch = "TRADING_MATCH"

	// SubjectMatch is the JetStream subject for durable match events.
	// Payload is a proto-encoded tradingv1.MatchEvent.
	SubjectMatch = "trading.match"

	// SubjectBookUpdatePrefix is the Core NATS subject prefix for ephemeral
	// book snapshot updates. Full subject is "trading.book.update.{market_id}".
	// Payload is JSON-encoded bookUpdatePayload.
	SubjectBookUpdatePrefix = "trading.book.update"

	// SubjectPriceUpdatePrefix is the Core NATS subject prefix for ephemeral
	// best-price updates. Full subject is "trading.price.update.{market_id}".
	// Payload is JSON-encoded priceUpdatePayload.
	SubjectPriceUpdatePrefix = "trading.price.update"
)

// EnsureMatchStream creates or updates the JetStream stream that carries
// durable match events. Must be called at trading service startup, before
// the engine begins accepting orders.
func EnsureMatchStream(nc *platformnats.Client) error {
	_, err := nc.EnsureStream(platformnats.StreamConfig{
		Name:     StreamNameMatch,
		Subjects: []string{SubjectMatch},
		MaxAge:   7 * 24 * time.Hour,
		Storage:  nats.FileStorage,
	})
	if err != nil {
		return fmt.Errorf("ensuring match stream: %w", err)
	}
	return nil
}

// bookLevel is a single price level in a book-update snapshot.
type bookLevel struct {
	Price int64 `json:"price"`
	Size  int64 `json:"size"`
}

// bookUpdatePayload is the JSON shape published to trading.book.update.{market}.
type bookUpdatePayload struct {
	MarketID string      `json:"market_id"`
	Bids     []bookLevel `json:"bids"`
	Asks     []bookLevel `json:"asks"`
}

// priceUpdatePayload is the JSON shape published to trading.price.update.{market}.
type priceUpdatePayload struct {
	MarketID       string `json:"market_id"`
	BestBid        int64  `json:"best_bid"`
	BestAsk        int64  `json:"best_ask"`
	LastTradePrice int64  `json:"last_trade_price"`
}

// publishMatch sends a proto-encoded MatchEvent to the durable trading.match
// JetStream subject. Trace context is automatically propagated via headers.
func publishMatch(ctx context.Context, nc *platformnats.Client, trade *trading.Trade) error {
	evt := &tradingv1.MatchEvent{
		MatchId:       trade.MatchID,
		MakerOrderId:  trade.MakerOrderID,
		TakerOrderId:  trade.TakerOrderID,
		MarketId:      trade.MarketID,
		Price:         trade.Price,
		Size:          trade.Size,
		MakerFee:      trade.MakerFee,
		TakerFee:      trade.TakerFee,
		MakerAddress:  trade.MakerAddress,
		TakerAddress:  trade.TakerAddress,
		TimestampUnix: trade.CreatedAt.Unix(),
	}

	data, err := proto.Marshal(evt)
	if err != nil {
		return fmt.Errorf("marshaling match event: %w", err)
	}

	if _, err := nc.JetStreamPublish(ctx, SubjectMatch, data); err != nil {
		return fmt.Errorf("publishing match event: %w", err)
	}
	return nil
}

// publishBookUpdate sends a JSON-encoded top-of-book snapshot to the
// ephemeral Core NATS subject trading.book.update.{market_id}. This is
// fan-out data for WebSocket clients; loss during a reconnect is acceptable.
func publishBookUpdate(ctx context.Context, nc *platformnats.Client, marketID string, bids, asks []bookLevel) error {
	payload := bookUpdatePayload{
		MarketID: marketID,
		Bids:     bids,
		Asks:     asks,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling book update: %w", err)
	}
	subject := SubjectBookUpdatePrefix + "." + marketID
	if err := nc.Publish(ctx, subject, data); err != nil {
		return fmt.Errorf("publishing book update: %w", err)
	}
	return nil
}

// publishPriceUpdate sends a JSON-encoded best-bid/best-ask snapshot to the
// ephemeral Core NATS subject trading.price.update.{market_id}.
func publishPriceUpdate(ctx context.Context, nc *platformnats.Client, marketID string, bestBid, bestAsk, lastTradePrice int64) error {
	payload := priceUpdatePayload{
		MarketID:       marketID,
		BestBid:        bestBid,
		BestAsk:        bestAsk,
		LastTradePrice: lastTradePrice,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling price update: %w", err)
	}
	subject := SubjectPriceUpdatePrefix + "." + marketID
	if err := nc.Publish(ctx, subject, data); err != nil {
		return fmt.Errorf("publishing price update: %w", err)
	}
	return nil
}
