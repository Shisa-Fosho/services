package sharednats

import (
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

// StreamConfig configures a JetStream stream.
type StreamConfig struct {
	Name     string
	Subjects []string
	MaxAge   time.Duration
	MaxBytes int64
	Storage  nats.StorageType // Default: nats.FileStorage
	Replicas int              // Default: 1
}

// EnsureStream creates or updates a JetStream stream (idempotent).
func (client *Client) EnsureStream(cfg StreamConfig) (*nats.StreamInfo, error) {
	if cfg.Name == "" {
		return nil, fmt.Errorf("creating stream: name is required")
	}
	if len(cfg.Subjects) == 0 {
		return nil, fmt.Errorf("creating stream %s: at least one subject is required", cfg.Name)
	}

	if cfg.Storage == 0 {
		cfg.Storage = nats.FileStorage
	}
	if cfg.Replicas == 0 {
		cfg.Replicas = 1
	}

	jsCfg := &nats.StreamConfig{
		Name:     cfg.Name,
		Subjects: cfg.Subjects,
		MaxAge:   cfg.MaxAge,
		MaxBytes: cfg.MaxBytes,
		Storage:  cfg.Storage,
		Replicas: cfg.Replicas,
	}

	// Try to update first; if stream doesn't exist, create it.
	info, err := client.jetStream.StreamInfo(cfg.Name)
	if err == nil && info != nil {
		info, err = client.jetStream.UpdateStream(jsCfg)
		if err != nil {
			return nil, fmt.Errorf("updating stream %s: %w", cfg.Name, err)
		}
		return info, nil
	}

	info, err = client.jetStream.AddStream(jsCfg)
	if err != nil {
		return nil, fmt.Errorf("creating stream %s: %w", cfg.Name, err)
	}
	return info, nil
}

// EnsureConsumer creates or updates a JetStream consumer (idempotent).
func (client *Client) EnsureConsumer(stream string, cfg *nats.ConsumerConfig) (*nats.ConsumerInfo, error) {
	if stream == "" {
		return nil, fmt.Errorf("creating consumer: stream name is required")
	}
	if cfg == nil {
		return nil, fmt.Errorf("creating consumer on %s: config is required", stream)
	}

	info, err := client.jetStream.AddConsumer(stream, cfg)
	if err != nil {
		return nil, fmt.Errorf("creating consumer %s on %s: %w", cfg.Durable, stream, err)
	}
	return info, nil
}
