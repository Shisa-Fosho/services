package sharednats

import (
	"errors"
	"fmt"

	"github.com/nats-io/nats.go"
)

// EnsureKeyValue returns the JetStream KV bucket described by cfg, creating
// it if it does not yet exist. Idempotent — safe to call on every service
// startup. Mirrors EnsureStream's "try get, fall back to create" pattern.
func (client *Client) EnsureKeyValue(cfg *nats.KeyValueConfig) (nats.KeyValue, error) {
	if cfg == nil {
		return nil, fmt.Errorf("creating KV bucket: config is required")
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("creating KV bucket: bucket name is required")
	}

	kv, err := client.jetStream.KeyValue(cfg.Bucket)
	if err == nil {
		return kv, nil
	}
	if !errors.Is(err, nats.ErrBucketNotFound) {
		return nil, fmt.Errorf("looking up KV bucket %s: %w", cfg.Bucket, err)
	}

	kv, err = client.jetStream.CreateKeyValue(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating KV bucket %s: %w", cfg.Bucket, err)
	}
	return kv, nil
}
