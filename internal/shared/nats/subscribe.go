package sharednats

import (
	"context"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.uber.org/zap"
)

// MessageHandler processes an incoming NATS message with trace context.
type MessageHandler func(ctx context.Context, msg *nats.Msg) error

// Subscribe creates a Core NATS subscription that extracts OpenTelemetry trace
// context from message headers before invoking the handler.
func (client *Client) Subscribe(subject string, handler MessageHandler) (*nats.Subscription, error) {
	return client.conn.Subscribe(subject, client.wrapHandler(handler))
}

// QueueSubscribe creates a queue group subscription that extracts OpenTelemetry
// trace context from message headers before invoking the handler.
func (client *Client) QueueSubscribe(subject, queue string, handler MessageHandler) (*nats.Subscription, error) {
	return client.conn.QueueSubscribe(subject, queue, client.wrapHandler(handler))
}

// JetStreamSubscribe creates a JetStream subscription that extracts trace
// context, calls the handler, and acks/naks based on the result.
func (client *Client) JetStreamSubscribe(subject string, handler MessageHandler, opts ...nats.SubOpt) (*nats.Subscription, error) {
	return client.jetStream.Subscribe(subject, func(msg *nats.Msg) {
		ctx := extractTraceContext(context.Background(), msg)

		if err := handler(ctx, msg); err != nil {
			client.logger.Error("JetStream handler error",
				zap.String("subject", subject),
				zap.Error(err),
			)
			if nakErr := msg.Nak(); nakErr != nil {
				client.logger.Warn("failed to nak message", zap.Error(nakErr))
			}
			return
		}
		if ackErr := msg.Ack(); ackErr != nil {
			client.logger.Warn("failed to ack message", zap.Error(ackErr))
		}
	}, opts...)
}

// wrapHandler returns a nats.MsgHandler that extracts trace context and
// invokes the MessageHandler.
func (client *Client) wrapHandler(handler MessageHandler) nats.MsgHandler {
	return func(msg *nats.Msg) {
		ctx := extractTraceContext(context.Background(), msg)

		if err := handler(ctx, msg); err != nil {
			client.logger.Error("NATS handler error",
				zap.String("subject", msg.Subject),
				zap.Error(err),
			)
		}
	}
}

// extractTraceContext reads OpenTelemetry trace context from NATS message headers.
func extractTraceContext(ctx context.Context, msg *nats.Msg) context.Context {
	if msg.Header == nil {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(msg.Header))
}
