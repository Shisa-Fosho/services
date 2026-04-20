package observability

import "github.com/prometheus/client_golang/prometheus"

// NewUnregisteredMetrics returns a *Metrics that is NOT registered with the
// global Prometheus registry. Intended for tests that need a *Metrics value
// but don't scrape /metrics. Calling NewMetrics multiple times in one test
// binary panics (MustRegister rejects duplicate descriptors with the same
// service label), so this helper lets tests construct as many as they need.
func NewUnregisteredMetrics(serviceName string) *Metrics {
	labels := prometheus.Labels{"service": serviceName}
	return &Metrics{
		RequestTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: "requests_total", ConstLabels: labels},
			[]string{"method", "status"},
		),
		RequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{Name: "request_duration_seconds", ConstLabels: labels},
			[]string{"method", "status"},
		),
		MatchesTotal:     prometheus.NewCounter(prometheus.CounterOpts{Name: "matches_total", ConstLabels: labels}),
		SettlementsTotal: prometheus.NewCounter(prometheus.CounterOpts{Name: "settlements_total", ConstLabels: labels}),
		DBErrorTotal:     prometheus.NewCounter(prometheus.CounterOpts{Name: "db_errors_total", ConstLabels: labels}),
		RateLimitRejectedTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: "ratelimit_rejected_total", ConstLabels: labels},
			[]string{"profile", "key_type"},
		),
		RateLimitLockoutTotal: prometheus.NewCounter(prometheus.CounterOpts{Name: "ratelimit_lockout_total", ConstLabels: labels}),
		RateLimitEvictedTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: "ratelimit_evicted_total", ConstLabels: labels},
			[]string{"profile", "key_type", "reason"},
		),
		RateLimitSweepDurationSeconds: prometheus.NewHistogram(
			prometheus.HistogramOpts{Name: "ratelimit_sweep_duration_seconds", ConstLabels: labels},
		),
	}
}
