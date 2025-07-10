package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	once           sync.Once
	RequestsTotal  *prometheus.CounterVec
	CacheHits      prometheus.Counter
	CacheEvictions prometheus.Counter

	// Remote fetcher metrics
	FetchAttemptsTotal *prometheus.CounterVec
	FetchSuccessTotal  prometheus.Counter
	FetchErrorsTotal   *prometheus.CounterVec
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

	// Remote fetcher metrics
	FetchAttemptsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "geoip_remote_fetch_attempts_total",
			Help: "Total number of remote fetch attempts",
		},
		[]string{"endpoint"},
	)
	FetchSuccessTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "geoip_remote_fetch_success_total",
			Help: "Total number of successful remote fetches",
		},
	)
	FetchErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "geoip_remote_fetch_errors_total",
			Help: "Total number of remote fetch errors by type",
		},
		[]string{"error_type"},
	)

	prometheus.MustRegister(RequestsTotal)
	prometheus.MustRegister(CacheHits)
	prometheus.MustRegister(CacheEvictions)
	prometheus.MustRegister(FetchAttemptsTotal)
	prometheus.MustRegister(FetchSuccessTotal)
	prometheus.MustRegister(FetchErrorsTotal)
}
