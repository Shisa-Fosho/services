package market

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	sharednats "github.com/Shisa-Fosho/services/internal/shared/nats"
)

// ConfigBucket is the JetStream KV bucket name carrying per-market config
// (status, token IDs, fee rate, trading parameters) for the trading service
// to consume into its in-memory order-book cache.
const ConfigBucket = "market-config"

// statusSubjectPrefix is the prefix for ephemeral status-change publishes
// fanned out to the WebSocket server.
const statusSubjectPrefix = "platform.market."

// ConfigEntry is the JSON payload written to the `market-config` KV bucket,
// keyed by market ID. Pointer fields are omitted from JSON when nil so
// consumers can distinguish "no per-market override" from "explicit zero".
type ConfigEntry struct {
	MarketID   string `json:"market_id"`
	Status     string `json:"status"`
	TokenIDYes string `json:"token_id_yes"`
	TokenIDNo  string `json:"token_id_no"`
	FeeRateBps *int64 `json:"fee_rate_bps,omitempty"`
	TickSize   string `json:"tick_size"`
	MinSize    int64  `json:"min_size"`
	MaxSize    *int64 `json:"max_size,omitempty"`
}

func toConfigEntry(market *Market) ConfigEntry {
	return ConfigEntry{
		MarketID:   market.ID,
		Status:     market.Status.String(),
		TokenIDYes: market.TokenIDYes,
		TokenIDNo:  market.TokenIDNo,
		FeeRateBps: market.FeeRateBps,
		TickSize:   market.TickSize.String(),
		MinSize:    market.MinSize,
		MaxSize:    market.MaxSize,
	}
}

// statusChangePayload is the body pushed onto platform.market.{id} for
// ephemeral fan-out by the WebSocket server. Status changes are not
// durably stored — clients receive only the current status (and outcome
// when the market reached a resolved-with-outcome state); the KV bucket
// is the durable source of truth.
type statusChangePayload struct {
	MarketID string  `json:"market_id"`
	Status   string  `json:"status"`
	Outcome  *string `json:"outcome,omitempty"`
}

// Publisher writes market-config updates to the `market-config` JetStream
// KV bucket and publishes status-change events on Core NATS for ephemeral
// WebSocket fan-out.
//
// All callers commit to the database first, then publish. The Publisher
// has no idempotency or retry logic of its own — KV puts are inherently
// idempotent (last write wins, keyed by market ID), and Core NATS publish
// failures bubble up to the caller for retry.
type Publisher struct {
	natsClient *sharednats.Client
	kv         nats.KeyValue
	logger     *zap.Logger
}

// NewPublisher binds a Publisher to a NATS client and a pre-resolved KV
// bucket. Callers obtain the bucket via sharednats.Client.EnsureKeyValue
// at startup.
func NewPublisher(client *sharednats.Client, kv nats.KeyValue, logger *zap.Logger) *Publisher {
	return &Publisher{natsClient: client, kv: kv, logger: logger}
}

// PublishMarketConfig writes the market's current config to the
// `market-config` KV bucket, keyed by market ID. No context parameter:
// nats.KeyValue.Put is synchronous and exposes neither cancellation nor
// header injection, so propagating ctx here would be misleading.
func (publisher *Publisher) PublishMarketConfig(market *Market) error {
	if market == nil {
		return fmt.Errorf("publishing market-config: market is nil")
	}
	entry := toConfigEntry(market)
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling market-config for %s: %w", market.ID, err)
	}
	if _, err := publisher.kv.Put(market.ID, data); err != nil {
		return fmt.Errorf("publishing market-config for %s: %w", market.ID, err)
	}
	return nil
}

// PublishStatusChange publishes the new status on platform.market.{id}
// for ephemeral WebSocket fan-out. Forwards to PublishStatusChangeWithOutcome
// with a nil outcome — convenience wrapper for non-resolve/void transitions.
func (publisher *Publisher) PublishStatusChange(ctx context.Context, marketID string, status Status) error {
	return publisher.PublishStatusChangeWithOutcome(ctx, marketID, status, nil)
}

// PublishStatusChangeWithOutcome publishes a status change, optionally
// carrying the final outcome (used on resolve so WebSocket clients see
// YES / NO without re-fetching). Core NATS — no durability — but errors
// are surfaced so callers can retry.
func (publisher *Publisher) PublishStatusChangeWithOutcome(ctx context.Context, marketID string, status Status, outcome *Outcome) error {
	if marketID == "" {
		return fmt.Errorf("publishing status-change: market_id is required")
	}
	subject := statusSubjectPrefix + marketID
	payload := statusChangePayload{
		MarketID: marketID,
		Status:   status.String(),
	}
	if outcome != nil {
		out := outcome.String()
		payload.Outcome = &out
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling status-change for %s: %w", marketID, err)
	}
	if err := publisher.natsClient.Publish(ctx, subject, data); err != nil {
		return fmt.Errorf("publishing status-change for %s: %w", marketID, err)
	}
	return nil
}
