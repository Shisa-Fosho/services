package sharednats

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestInjectTraceContext(t *testing.T) {
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

	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-op")
	defer span.End()

	msg := &nats.Msg{Header: make(nats.Header)}
	injectTraceContext(ctx, msg)

	// HeaderCarrier canonicalizes keys, so check for "Traceparent".
	if len(msg.Header.Values("Traceparent")) == 0 {
		t.Error("expected Traceparent header to be set")
	}
}

func TestInjectTraceContext_NilHeader(t *testing.T) {
	t.Parallel()

	msg := &nats.Msg{}
	injectTraceContext(context.Background(), msg)

	if msg.Header == nil {
		t.Error("expected header to be initialized, got nil")
	}
}
