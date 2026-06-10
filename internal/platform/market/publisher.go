package market

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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

// publishAttempts and publishRetryDelay bound the in-request retry on
// publish failures. Both publish operations are idempotent (KV put is
// last-write-wins; the status broadcast is safe to repeat), so retrying
// a transient NATS blip here spares the admin a manual retry. A failure
// that survives all attempts still bubbles up as a 502.
const (
	publishAttempts   = 3
	publishRetryDelay = 100 * time.Millisecond
)

// retryOperation runs operation up to attempts times, sleeping delay
// between tries. Returns nil on the first success, or the last error.
func retryOperation(attempts int, delay time.Duration, operation func() error) error {
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			time.Sleep(delay)
		}
		if err = operation(); err == nil {
			return nil
		}
	}
	return err
}

// Publisher writes market-config updates to the `market-config` JetStream
// KV bucket and publishes status-change events on Core NATS for ephemeral
// WebSocket fan-out. It also exposes the read side of the same bucket
// (LiveStatus) so admin reads can report the state trading actually sees.
//
// All callers commit to the database first, then publish. Both publish
// methods retry transient failures in-request (see publishAttempts);
// errors that survive the retries bubble up to the caller, whose own
// retry is safe because the repository transitions are idempotent.
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
	err = retryOperation(publishAttempts, publishRetryDelay, func() error {
		_, putErr := publisher.kv.Put(market.ID, data)
		return putErr
	})
	if err != nil {
		return fmt.Errorf("publishing market-config for %s: %w", market.ID, err)
	}
	return nil
}

// LiveStatus reads the market's entry from the `market-config` KV bucket
// and returns its status string — the state the trading service actually
// operates on. Returns ok=false (and no error) when the market has no
// entry, i.e. its config has never been successfully published.
func (publisher *Publisher) LiveStatus(marketID string) (string, bool, error) {
	entry, err := publisher.kv.Get(marketID)
	if err != nil {
		if errors.Is(err, nats.ErrKeyNotFound) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("reading market-config for %s: %w", marketID, err)
	}
	var config ConfigEntry
	if err := json.Unmarshal(entry.Value(), &config); err != nil {
		return "", false, fmt.Errorf("decoding market-config for %s: %w", marketID, err)
	}
	return config.Status, true, nil
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
	err = retryOperation(publishAttempts, publishRetryDelay, func() error {
		return publisher.natsClient.Publish(ctx, subject, data)
	})
	if err != nil {
		return fmt.Errorf("publishing status-change for %s: %w", marketID, err)
	}
	return nil
}
