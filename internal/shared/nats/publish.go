package sharednats

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// Publish sends a message on Core NATS with OpenTelemetry trace context
// injected into the message headers.
func (c *Client) Publish(ctx context.Context, subject string, data []byte) error {
	msg := &nats.Msg{
		Subject: subject,
		Data:    data,
		Header:  make(nats.Header),
	}

	injectTraceContext(ctx, msg)

	if err := c.conn.PublishMsg(msg); err != nil {
		return fmt.Errorf("publishing to %s: %w", subject, err)
	}
	return nil
}

// JetStreamPublish sends a message via JetStream with OpenTelemetry trace
// context injected into the message headers.
func (c *Client) JetStreamPublish(ctx context.Context, subject string, data []byte, opts ...nats.PubOpt) (*nats.PubAck, error) {
	msg := &nats.Msg{
		Subject: subject,
		Data:    data,
		Header:  make(nats.Header),
	}

	injectTraceContext(ctx, msg)

	ack, err := c.js.PublishMsg(msg, opts...)
	if err != nil {
		return nil, fmt.Errorf("JetStream publishing to %s: %w", subject, err)
	}
	return ack, nil
}

// injectTraceContext propagates OpenTelemetry trace context into NATS message headers.
func injectTraceContext(ctx context.Context, msg *nats.Msg) {
	if msg.Header == nil {
		msg.Header = make(nats.Header)
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(msg.Header))
}
