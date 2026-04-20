package ratelimit

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// testCounterValue reads the current value of a Prometheus counter. Useful
// for asserting that a metric fired without scraping /metrics.
func testCounterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		t.Fatalf("reading counter: %v", err)
	}
	return m.GetCounter().GetValue()
}
