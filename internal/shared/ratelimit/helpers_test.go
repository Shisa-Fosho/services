package ratelimit

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// testCounterValue reads the current value of a Prometheus counter. Useful
// for asserting that a metric fired without scraping /metrics.
func testCounterValue(t *testing.T, counter prometheus.Counter) float64 {
	t.Helper()
	var metric dto.Metric
	if err := counter.Write(&metric); err != nil {
		t.Fatalf("reading counter: %v", err)
	}
	return metric.GetCounter().GetValue()
}
