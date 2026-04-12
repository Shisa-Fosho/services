// Package engine implements the in-memory CLOB matching engine for the
// trading service. The engine takes validated orders, matches them using
// price-time priority on a unified YES-side order book, persists the
// results via the trading repository, and publishes trade events to NATS.
package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Shisa-Fosho/services/internal/trading"
)

// ErrMarketNotFound is returned when a market config is requested for a
// market the provider does not know about. The engine treats this as a
// validation error and rejects the order.
var ErrMarketNotFound = errors.New("market not found")

// MarketConfigProvider supplies per-market configuration to the matching
// engine. The engine calls GetMarketConfig on every order placement and
// during book rebuild. Implementations must be safe for concurrent use.
//
// The interface exists so the engine can be unit-tested with a simple map
// and later swapped for a NATS KV-backed implementation without engine
// code changes (see issue T2c).
type MarketConfigProvider interface {
	GetMarketConfig(ctx context.Context, marketID string) (trading.MarketConfig, error)
}

// StaticProvider is an in-memory MarketConfigProvider backed by a map.
// It is used for unit tests and as a temporary stub in cmd/trading/main.go
// until the real NATS KV-backed provider lands (T2c).
type StaticProvider struct {
	mu      sync.RWMutex
	configs map[string]trading.MarketConfig
}

// NewStaticProvider returns a StaticProvider seeded with the given configs.
// The input map is copied; callers may safely mutate their copy afterwards.
func NewStaticProvider(configs map[string]trading.MarketConfig) *StaticProvider {
	copied := make(map[string]trading.MarketConfig, len(configs))
	for k, v := range configs {
		copied[k] = v
	}
	return &StaticProvider{configs: copied}
}

// GetMarketConfig returns the config for the given market, or ErrMarketNotFound.
func (p *StaticProvider) GetMarketConfig(_ context.Context, marketID string) (trading.MarketConfig, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	cfg, ok := p.configs[marketID]
	if !ok {
		return trading.MarketConfig{}, fmt.Errorf("market %s: %w", marketID, ErrMarketNotFound)
	}
	return cfg, nil
}

// Set inserts or replaces a config. Used by tests.
func (p *StaticProvider) Set(cfg trading.MarketConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.configs[cfg.MarketID] = cfg
}
