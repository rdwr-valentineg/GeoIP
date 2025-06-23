package metrics

import (
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	once           sync.Once
	RequestsTotal  *prometheus.CounterVec
	CacheHits      prometheus.Counter
	CacheEvictions prometheus.Counter
)

func InitMetrics() {
	once.Do(func() {
		registerMetrics()
	})
}

func registerMetrics() {
	RequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "geoip_auth_requests_total",
			Help: "Total number of auth requests",
		},
		[]string{"country", "allowed"},
	)
	CacheHits = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "geoip_auth_cache_hits_total",
			Help: "Total number of cache hits",
		},
	)
	CacheEvictions = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "geoip_auth_cache_evictions_total",
			Help: "Total number of cache purges",
		},
	)

	prometheus.MustRegister(RequestsTotal)
	prometheus.MustRegister(CacheHits)
	prometheus.MustRegister(CacheEvictions)

	http.Handle("/metrics", promhttp.Handler())
}
