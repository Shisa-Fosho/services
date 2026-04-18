package sharednats

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestExtractTraceContext(t *testing.T) {
	// Not parallel: mutates global OTel state.
	tp := sdktrace.NewTracerProvider()
	defer func() { _ = tp.Shutdown(context.Background()) }()

	prevTP := otel.GetTracerProvider()
	prevProp := otel.GetTextMapPropagator()
	defer func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	}()

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// Set a valid traceparent header with canonical key (as HeaderCarrier writes it).
	msg := &nats.Msg{Header: nats.Header{}}
	msg.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

	ctx := extractTraceContext(context.Background(), msg)

	// Re-inject and verify round-trip.
	outMsg := &nats.Msg{Header: make(nats.Header)}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(outMsg.Header))

	if len(outMsg.Header.Values("Traceparent")) == 0 {
		t.Error("expected Traceparent to round-trip through extract/inject")
	}
}

func TestExtractTraceContext_NilHeader(t *testing.T) {
	t.Parallel()

	msg := &nats.Msg{}
	ctx := extractTraceContext(context.Background(), msg)
	if ctx == nil {
		t.Error("expected non-nil context, got nil")
	}
}
