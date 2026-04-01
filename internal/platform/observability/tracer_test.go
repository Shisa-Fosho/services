package observability

import (
	"testing"
)

func TestTracerConfig_Defaults(t *testing.T) {
	t.Parallel()

	cfg := TracerConfig{
		ServiceName: "test-service",
	}

	if cfg.ServiceName != "test-service" {
		t.Errorf("ServiceName = %v, want test-service", cfg.ServiceName)
	}
	// Defaults are applied inside NewTracer, verify they're empty before.
	if cfg.OTLPEndpoint != "" {
		t.Errorf("OTLPEndpoint = %v, want empty (defaults applied in NewTracer)", cfg.OTLPEndpoint)
	}
	if cfg.SampleRate != 0 {
		t.Errorf("SampleRate = %v, want 0 (defaults applied in NewTracer)", cfg.SampleRate)
	}
}
