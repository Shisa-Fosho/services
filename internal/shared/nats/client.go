// Package sharednats provides NATS client utilities with JetStream support
// and OpenTelemetry trace context propagation for all Shisa services.
package sharednats

import (
	"fmt"
	"os"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// ClientConfig configures the NATS client.
type ClientConfig struct {
	URL    string      // Default: nats.DefaultURL ("nats://127.0.0.1:4222")
	Name   string      // Connection name for monitoring.
	Logger *zap.Logger // Logger for disconnect/reconnect events.
}

// Client wraps a NATS connection and JetStream context.
type Client struct {
	conn      *nats.Conn
	jetStream nats.JetStreamContext
	logger    *zap.Logger
}

// NewClient connects to NATS, creates a JetStream context, and sets up
// disconnect/reconnect event handlers.
func NewClient(cfg ClientConfig) (*Client, error) {
	if cfg.URL == "" {
		cfg.URL = nats.DefaultURL
	}
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}

	logger := cfg.Logger

	opts := []nats.Option{
		nats.Name(cfg.Name),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			logger.Warn("NATS disconnected", zap.Error(err))
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			logger.Info("NATS reconnected", zap.String("url", nc.ConnectedUrl()))
		}),
		nats.ClosedHandler(func(_ *nats.Conn) {
			logger.Info("NATS connection closed")
		}),
	}

	conn, err := nats.Connect(cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("connecting to NATS at %s: %w", cfg.URL, err)
	}

	jetStream, err := conn.JetStream()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("creating JetStream context: %w", err)
	}

	return &Client{
		conn:      conn,
		jetStream: jetStream,
		logger:    logger,
	}, nil
}

// ClientFromEnv creates a NATS client using the NATS_URL environment variable.
func ClientFromEnv(logger *zap.Logger, name string) (*Client, error) {
	url := os.Getenv("NATS_URL")
	return NewClient(ClientConfig{
		URL:    url,
		Name:   name,
		Logger: logger,
	})
}

// Conn returns the underlying NATS connection.
func (client *Client) Conn() *nats.Conn {
	return client.conn
}

// JetStream returns the JetStream context.
func (client *Client) JetStream() nats.JetStreamContext {
	return client.jetStream
}

// Close drains the connection and then closes it.
func (client *Client) Close() {
	if err := client.conn.Drain(); err != nil {
		client.logger.Warn("error draining NATS connection", zap.Error(err))
	}
}

// Drain initiates a graceful drain of the connection.
func (client *Client) Drain() error {
	return client.conn.Drain()
}
