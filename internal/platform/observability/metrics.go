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

	prometheus.MustRegister(requestTotal, requestDuration, matchesTotal, settlementsTotal, dbErrorTotal)

	return &Metrics{
		RequestTotal:     requestTotal,
		RequestDuration:  requestDuration,
		MatchesTotal:     matchesTotal,
		SettlementsTotal: settlementsTotal,
		DBErrorTotal:     dbErrorTotal,
	}
}

// Handler returns the Prometheus metrics HTTP handler.
func (m *Metrics) Handler() http.Handler {
	return promhttp.Handler()
}
