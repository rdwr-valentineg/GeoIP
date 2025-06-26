package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRegisterMetrics(t *testing.T) {
	InitMetrics()

	if RequestsTotal == nil {
		t.Fatal("RequestsTotal should not be nil after registerMetrics")
	}
	if CacheHits == nil {
		t.Fatal("CacheHits should not be nil after registerMetrics")
	}
	if CacheEvictions == nil {
		t.Fatal("CacheEvictions should not be nil after registerMetrics")
	}

	// Test RequestsTotal labels
	labels := prometheus.Labels{"country": "US", "allowed": "true"}
	RequestsTotal.With(labels).Inc()
	val := testutil.ToFloat64(RequestsTotal.With(labels))
	if val != 1 {
		t.Errorf("Expected RequestsTotal with labels to be 1, got %v", val)
	}

	// Test CacheHits counter
	CacheHits.Inc()
	if testutil.ToFloat64(CacheHits) != 1 {
		t.Errorf("Expected CacheHits to be 1, got %v", testutil.ToFloat64(CacheHits))
	}

	// Test CacheEvictions counter
	CacheEvictions.Add(2)
	if testutil.ToFloat64(CacheEvictions) != 2 {
		t.Errorf("Expected CacheEvictions to be 2, got %v", testutil.ToFloat64(CacheEvictions))
	}
}
