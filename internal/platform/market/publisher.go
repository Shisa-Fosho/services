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
// (status, token IDs, fee rate) for the trading service to consume into
// its in-memory order-book cache.
const ConfigBucket = "market-config"

// statusSubjectPrefix is the prefix for ephemeral status-change publishes
// fanned out to the WebSocket server.
const statusSubjectPrefix = "platform.market."

// ConfigEntry is the JSON payload written to the `market-config` KV
// bucket, keyed by market ID. The trading service reads this on startup
// (full scan) and watches for changes (incremental updates).
//
// TODO(human): define the struct fields with JSON tags.
//
// The issue specifies five required fields:
//   - market_id      (string)  — UUID, the KV key but also embedded in payload for ergonomics
//   - status         (string)  — one of ACTIVE / PAUSED / RESOLVED / VOIDED, from Status.String()
//   - token_id_yes   (string)  — Conditional token ID for YES outcome
//   - token_id_no    (string)  — Conditional token ID for NO outcome
//   - fee_rate_bps   (?)       — see design decision below
//
// Design decision: should fee_rate_bps be `*int64` (pointer, distinguishes
// "use platform default" from "explicit 0 bps") or `int64` (treats nil/0
// equivalently because per the issue NULL means 0)? The Market struct uses
// *int64. Either is defensible — match Market for honesty about NULL, or
// simplify for trading-side consumers who just want a number to apply.
//
// Once you've defined the fields, fill in toConfigEntry below to
// populate them from a *Market.
type ConfigEntry struct {
	MarketID   string `json:"market_id"`
	Status     string `json:"status"`
	TokenIDYes string `json:"token_id_yes"`
	TokenIDNo  string `json:"token_id_no"`
	FeeRateBps *int64 `json:"fee_rate_bps,omitempty"`
}

// toConfigEntry maps a Market into the KV payload shape. Lives next
// to the struct definition so the mapping evolves alongside the schema.
func toConfigEntry(market *Market) ConfigEntry {
	return ConfigEntry{
		MarketID:   market.ID,
		Status:     market.Status.String(),
		TokenIDYes: market.TokenIDYes,
		TokenIDNo:  market.TokenIDNo,
		FeeRateBps: market.FeeRateBps,
	}
}

// statusChangePayload is the body pushed onto platform.market.{id} for
// ephemeral fan-out by the WebSocket server. Status changes are not
// durably stored — clients receive only the current status; the KV
// bucket is the durable source of truth.
type statusChangePayload struct {
	MarketID string `json:"market_id"`
	Status   string `json:"status"`
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
// for ephemeral WebSocket fan-out. Core NATS — no durability — but
// errors are surfaced so callers can retry.
func (publisher *Publisher) PublishStatusChange(ctx context.Context, marketID string, status Status) error {
	if marketID == "" {
		return fmt.Errorf("publishing status-change: market_id is required")
	}
	subject := statusSubjectPrefix + marketID
	data, err := json.Marshal(statusChangePayload{
		MarketID: marketID,
		Status:   status.String(),
	})
	if err != nil {
		return fmt.Errorf("marshaling status-change for %s: %w", marketID, err)
	}
	if err := publisher.natsClient.Publish(ctx, subject, data); err != nil {
		return fmt.Errorf("publishing status-change for %s: %w", marketID, err)
	}
	return nil
}
