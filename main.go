package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/oschwald/geoip2-golang"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type (
	cacheEntry struct {
		allowed bool
		country string
	}
	AuthResponse struct {
		Message string `json:"message,omitempty"`
	}
)

var (
	geoCache = make(map[string]cacheEntry)
	cacheMux = sync.RWMutex{}

	excludeSubnets []*net.IPNet
	allowedCountries = map[string]bool{}

	logLevel         = "info"
	cachePurgePeriod time.Duration
	requestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "geoip_auth_requests_total",
			Help: "Total number of auth requests",
		},
		[]string{"country", "allowed"},
	)
	cacheHits = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "geoip_auth_cache_hits_total",
			Help: "Total number of cache hits",
		},
	)
	cacheEvictions = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "geoip_auth_cache_evictions_total",
			Help: "Total number of cache purges",
		},
	)
)

func initMetrics() {
	prometheus.MustRegister(requestsTotal)
	prometheus.MustRegister(cacheHits)
	prometheus.MustRegister(cacheEvictions)
}

func logInfo(format string, args ...interface{}) {
	if logLevel == "info" || logLevel == "debug" {
		log.Printf("[INFO] "+format, args...)
	}
}

func logError(format string, args ...interface{}) {
	if logLevel != "none" {
		log.Printf("[ERROR] "+format, args...)
	}
}

func isExcluded(ip net.IP, excluded []*net.IPNet) bool {
	for _, subnet := range excluded {
		if subnet.Contains(ip) {
			return true
		}
	}
	return false
}

func respondAllowed(w http.ResponseWriter, isoCode string) {
	w.Header().Set("X-Country", isoCode)
	w.WriteHeader(http.StatusOK)
}

func clearCachePeriodically(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			cacheMux.Lock()
			evicted := len(geoCache)
			geoCache = make(map[string]cacheEntry)
			cacheMux.Unlock()
			cacheEvictions.Add(float64(evicted))
			logInfo("Cache cleared, evicted %d entries", evicted)
		}
	}()
}

func handleRequest(w http.ResponseWriter, r *http.Request, db *geoip2.Reader, ipHeader string, excluded []*net.IPNet) {
	ipStr := r.Header.Get(ipHeader)
	if ipStr == "" {
		ipStr, _, _ = net.SplitHostPort(r.RemoteAddr)
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		http.Error(w, "Forbidden (invalid IP)", http.StatusForbidden)
		logError("Invalid IP: %v", ipStr)
		return
	}

	if isExcluded(ip, excluded) {
		logInfo("Excluded IP allowed: %s", ipStr)
		respondAllowed(w, "LAN")
		requestsTotal.WithLabelValues("LAN", "true").Inc()
		return
	}

	cacheMux.RLock()
	entry, found := geoCache[ipStr]
	cacheMux.RUnlock()

	if found {
		logInfo("Cache hit for IP: %s", ipStr)
		cacheHits.Inc()
		if entry.allowed {
			respondAllowed(w, entry.country)
			requestsTotal.WithLabelValues(entry.country, "true").Inc()
			return
		}
		http.Error(w, "Forbidden", http.StatusForbidden)
		requestsTotal.WithLabelValues(entry.country, "false").Inc()
		return
	}

	country, err := db.Country(ip)
	if err != nil {
		http.Error(w, "Forbidden", http.StatusForbidden)
		logError("GeoIP lookup failed for IP %s: %v", ipStr, err)
		return
	}

	isoCode := country.Country.IsoCode
	allowed := allowedCountries[isoCode]

	cacheMux.Lock()
	geoCache[ipStr] = cacheEntry{
		allowed: allowed,
		country: isoCode,
	}
	cacheMux.Unlock()

	requestsTotal.WithLabelValues(isoCode, fmt.Sprintf("%v", allowed)).Inc()

	if allowed {
		logInfo("Allowed country %s for IP %s", isoCode, ipStr)
		respondAllowed(w, isoCode)
	} else {
		logInfo("Denied country %s for IP %s", isoCode, ipStr)
		w.Header().Set("X-Country", isoCode)
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(AuthResponse{Message: "Access denied"})
	}
}

func main() {
	var (
		dbPath = flag.String("db", getEnv("GEOIP_DB", "/mmdb/GeoLite2-Country.mmdb"), "Path to MaxMind GeoIP2 DB")
		port = flag.String("port", getEnv("PORT", "8080"), "Port to listen on")
		excludeCIDR = flag.String("exclude", getEnv("EXCLUDE_CIDRS", "192.168.0.0/16,10.0.0.0/8,172.16.0.0/12,127.0.0.0/8,::1/128"), "Comma-separated CIDRs to exclude")
		allowedCountryList = flag.String("allow", getEnv("ALLOW_COUNTRIES", "US"), "Comma-separated list of ISO country codes to allow")
		ipHeader = flag.String("ip-header", getEnv("IP_HEADER", "X-Forwarded-For"), "Header to extract real IP")
		logLevelFlag = flag.String("log-level", getEnv("LOG_LEVEL", "info"), "Log level (none, error, info, debug)")
		purgeInterval = flag.Duration("purge-interval", mustParseDuration(getEnv("CACHE_PURGE_INTERVAL", "2m")), "Interval for clearing the cache")
	)
	flag.Parse()
	logLevel = *logLevelFlag
	cachePurgePeriod = *purgeInterval

	for _, cidr := range strings.Split(*excludeCIDR, ",") {
		cidr = strings.TrimSpace(cidr)
		_, ipnet, err := net.ParseCIDR(cidr)
		if err == nil {
			excludeSubnets = append(excludeSubnets, ipnet)
		}
	}

	for _, c := range strings.Split(*allowedCountryList, ",") {
		allowedCountries[strings.ToUpper(strings.TrimSpace(c))] = true
	}

	db, err := geoip2.Open(*dbPath)
	if err != nil {
		log.Fatalf("[FATAL] Could not open GeoIP DB: %v", err)
	}
	defer db.Close()

	initMetrics()
	clearCachePeriodically(cachePurgePeriod)

	http.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		handleRequest(w, r, db, *ipHeader, excludeSubnets)
	})

	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	http.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready"))
	})

	http.Handle("/metrics", promhttp.Handler())

	logInfo("GeoIP ForwardAuth server listening on port %s", *port)
	if err := http.ListenAndServe(":"+*port, nil); err != nil {
		log.Fatalf("[FATAL] Server failed: %v", err)
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func mustParseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		log.Fatalf("[FATAL] Invalid duration: %v", err)
	}
	return d
}
