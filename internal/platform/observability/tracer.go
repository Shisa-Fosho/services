package observability

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TracerConfig configures the OpenTelemetry tracer provider.
type TracerConfig struct {
	ServiceName  string
	OTLPEndpoint string  // Default: "localhost:4317"
	Insecure     bool    // Default: true (for local dev)
	SampleRate   float64 // Default: 1.0
}

// NewTracer creates an OpenTelemetry TracerProvider with an OTLP gRPC exporter,
// registers it as the global provider, and sets up W3C TraceContext propagation.
// The returned shutdown function must be called on service termination to flush spans.
func NewTracer(ctx context.Context, cfg TracerConfig) (shutdown func(context.Context) error, err error) {
	if cfg.OTLPEndpoint == "" {
		cfg.OTLPEndpoint = "localhost:4317"
	}
	if cfg.SampleRate == 0 {
		cfg.SampleRate = 1.0
	}

	var dialOpts []grpc.DialOption
	if cfg.Insecure {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
		otlptracegrpc.WithDialOption(dialOpts...),
	)
	if err != nil {
		return nil, fmt.Errorf("creating OTLP exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("creating resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(cfg.SampleRate)),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}

// TracerFromEnv creates a tracer from environment variables.
// Reads OTEL_EXPORTER_OTLP_ENDPOINT (default "localhost:4317").
func TracerFromEnv(ctx context.Context, serviceName string) (func(context.Context) error, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	return NewTracer(ctx, TracerConfig{
		ServiceName:  serviceName,
		OTLPEndpoint: endpoint,
		Insecure:     true,
	})
}
