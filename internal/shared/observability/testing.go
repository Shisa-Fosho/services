package observability

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// NewNopLogger returns a no-op logger that discards all output.
func NewNopLogger() *zap.Logger {
	return zap.NewNop()
}

// NewTestLogger returns a logger that writes to the test's log output.
func NewTestLogger(t testing.TB) *zap.Logger {
	return zaptest.NewLogger(t)
}

// NewTestMetrics creates a Metrics instance backed by a custom registry
// so tests don't collide with the global Prometheus registry.
func NewTestMetrics() *Metrics {
	reg := prometheus.NewRegistry()

	requestTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "test_requests_total"},
		[]string{"method", "status"},
	)
	requestDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{Name: "test_request_duration_seconds", Buckets: prometheus.DefBuckets},
		[]string{"method", "status"},
	)
	matchesTotal := prometheus.NewCounter(prometheus.CounterOpts{Name: "test_matches_total"})
	settlementsTotal := prometheus.NewCounter(prometheus.CounterOpts{Name: "test_settlements_total"})
	dbErrorTotal := prometheus.NewCounter(prometheus.CounterOpts{Name: "test_db_errors_total"})

	reg.MustRegister(requestTotal, requestDuration, matchesTotal, settlementsTotal, dbErrorTotal)

	return &Metrics{
		RequestTotal:     requestTotal,
		RequestDuration:  requestDuration,
		MatchesTotal:     matchesTotal,
		SettlementsTotal: settlementsTotal,
		DBErrorTotal:     dbErrorTotal,
	}
}
