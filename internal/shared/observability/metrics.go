package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds Prometheus metrics common to all services.
type Metrics struct {
	RequestTotal    *prometheus.CounterVec   // Total requests by method and status.
	RequestDuration *prometheus.HistogramVec // Request latency by method and status.

	MatchesTotal     prometheus.Counter // Matched trades.
	SettlementsTotal prometheus.Counter // On-chain settlements.
	DBErrorTotal     prometheus.Counter // Database errors.

	RateLimitRejectedTotal        *prometheus.CounterVec // Rejected requests by profile and key type.
	RateLimitLockoutTotal         prometheus.Counter     // Lockouts triggered.
	RateLimitEvictedTotal         *prometheus.CounterVec // Entries evicted by profile, key type, and reason (cap|sweep).
	RateLimitSweepDurationSeconds prometheus.Histogram   // Sweeper run duration; climb signals need for sharding.
}

// NewMetrics creates and registers metrics for a service.
func NewMetrics(serviceName string) *Metrics {
	requestTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "requests_total",
			Help:        "Total number of requests",
			ConstLabels: prometheus.Labels{"service": serviceName},
		},
		[]string{"method", "status"},
	)

	requestDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:        "request_duration_seconds",
			Help:        "Request duration in seconds",
			Buckets:     prometheus.DefBuckets,
			ConstLabels: prometheus.Labels{"service": serviceName},
		},
		[]string{"method", "status"},
	)

	matchesTotal := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "matches_total",
		Help:        "Total number of matched trades",
		ConstLabels: prometheus.Labels{"service": serviceName},
	})

	settlementsTotal := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "settlements_total",
		Help:        "Total number of on-chain settlements",
		ConstLabels: prometheus.Labels{"service": serviceName},
	})

	dbErrorTotal := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "db_errors_total",
		Help:        "Total number of database errors",
		ConstLabels: prometheus.Labels{"service": serviceName},
	})

	rateLimitRejectedTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "ratelimit_rejected_total",
			Help:        "Requests rejected by the rate limiter",
			ConstLabels: prometheus.Labels{"service": serviceName},
		},
		[]string{"profile", "key_type"},
	)

	rateLimitLockoutTotal := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "ratelimit_lockout_total",
		Help:        "Lockouts triggered after repeated auth failures",
		ConstLabels: prometheus.Labels{"service": serviceName},
	})

	rateLimitEvictedTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "ratelimit_evicted_total",
			Help:        "Rate limiter entries evicted",
			ConstLabels: prometheus.Labels{"service": serviceName},
		},
		[]string{"profile", "key_type", "reason"},
	)

	rateLimitSweepDurationSeconds := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:        "ratelimit_sweep_duration_seconds",
		Help:        "Duration of rate limiter sweeper runs",
		Buckets:     []float64{0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1},
		ConstLabels: prometheus.Labels{"service": serviceName},
	})

	prometheus.MustRegister(
		requestTotal, requestDuration,
		matchesTotal, settlementsTotal, dbErrorTotal,
		rateLimitRejectedTotal, rateLimitLockoutTotal,
		rateLimitEvictedTotal, rateLimitSweepDurationSeconds,
	)

	return &Metrics{
		RequestTotal:                  requestTotal,
		RequestDuration:               requestDuration,
		MatchesTotal:                  matchesTotal,
		SettlementsTotal:              settlementsTotal,
		DBErrorTotal:                  dbErrorTotal,
		RateLimitRejectedTotal:        rateLimitRejectedTotal,
		RateLimitLockoutTotal:         rateLimitLockoutTotal,
		RateLimitEvictedTotal:         rateLimitEvictedTotal,
		RateLimitSweepDurationSeconds: rateLimitSweepDurationSeconds,
	}
}

// Handler returns the Prometheus metrics HTTP handler.
func (metrics *Metrics) Handler() http.Handler {
	return promhttp.Handler()
}
