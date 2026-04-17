package observability

import (
	"testing"
)

func TestNewTestMetrics(t *testing.T) {
	t.Parallel()

	metrics := NewTestMetrics()

	if metrics == nil {
		t.Fatal("NewTestMetrics returned nil")
	}
	if metrics.RequestTotal == nil {
		t.Error("RequestTotal is nil")
	}
	if metrics.RequestDuration == nil {
		t.Error("RequestDuration is nil")
	}
	if metrics.MatchesTotal == nil {
		t.Error("MatchesTotal is nil")
	}
	if metrics.SettlementsTotal == nil {
		t.Error("SettlementsTotal is nil")
	}
	if metrics.DBErrorTotal == nil {
		t.Error("DBErrorTotal is nil")
	}

	// Verify counters can be incremented without panic.
	metrics.MatchesTotal.Inc()
	metrics.SettlementsTotal.Inc()
	metrics.DBErrorTotal.Inc()
	metrics.RequestTotal.WithLabelValues("TestMethod", "OK").Inc()
	metrics.RequestDuration.WithLabelValues("TestMethod", "OK").Observe(0.5)

	// Verify handler is available.
	handler := metrics.Handler()
	if handler == nil {
		t.Error("Handler() returned nil")
	}
}
