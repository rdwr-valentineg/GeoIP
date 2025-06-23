package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/oschwald/geoip2-golang"
	"github.com/rdwr-valentineg/GeoIP/internal/config"
	"github.com/rdwr-valentineg/GeoIP/internal/metrics"
	"github.com/rs/zerolog/log"
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

	excludeSubnets   []*net.IPNet
	allowedCountries = map[string]bool{}
)

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
			metrics.CacheEvictions.Add(float64(evicted))
			log.Info().Int("evicted entries", evicted).Msg("Cache cleared")
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
		log.Debug().Str("ip", ipStr).Msg("Excluded IP allowed")
		return
	}

	if isExcluded(ip, excluded) {
		log.Debug().Str("ip", ipStr).Msg("Excluded IP allowed")
		respondAllowed(w, "LAN")
		metrics.RequestsTotal.WithLabelValues("LAN", "true").Inc()
		return
	}

	cacheMux.RLock()
	entry, found := geoCache[ipStr]
	cacheMux.RUnlock()

	if found {
		log.Debug().
			Str("ip", ipStr).
			Str("country", entry.country).
			Msg("Cache hit for")
		metrics.CacheHits.Inc()
		if entry.allowed {
			respondAllowed(w, entry.country)
			metrics.RequestsTotal.WithLabelValues(entry.country, "true").Inc()
			return
		}
		http.Error(w, "Forbidden", http.StatusForbidden)
		metrics.RequestsTotal.WithLabelValues(entry.country, "false").Inc()
		return
	}

	country, err := db.Country(ip)
	if err != nil {
		http.Error(w, "Forbidden", http.StatusForbidden)
		log.Info().Str("ip", ipStr).Err(err).Msg("GeoIP lookup failed")
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

	metrics.RequestsTotal.WithLabelValues(isoCode, fmt.Sprintf("%v", allowed)).Inc()

	if allowed {
		log.Debug().Str("ip", ipStr).Str("country", isoCode).Msg("Allowed")
		respondAllowed(w, isoCode)
	} else {
		log.Debug().Str("ip", ipStr).Str("country", isoCode).Msg("Denied")
		w.Header().Set("X-Country", isoCode)
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(AuthResponse{Message: "Access denied"})
	}
}

func main() {
	cfg, err := config.InitConfig()
	if err != nil {
		log.Fatal().Err(err).Msg("Invalid configuration")
	}

	InitLogger(cfg.LogLevelFlag)

	for cidr := range strings.SplitSeq(cfg.ExcludeCIDR, ",") {
		_, ipnet, err := net.ParseCIDR(strings.TrimSpace(cidr))
		if err == nil {
			excludeSubnets = append(excludeSubnets, ipnet)
		}
	}

	for c := range strings.SplitSeq(cfg.AllowedCountryList, ",") {
		allowedCountries[strings.ToUpper(strings.TrimSpace(c))] = true
	}

	db, err := geoip2.Open(cfg.DbPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Could not open GeoIP DB")
	}
	defer db.Close()

	addHandlers(db, cfg.IpHeader, excludeSubnets)
	metrics.InitMetrics()
	clearCachePeriodically(cfg.CachePurgePeriod)

	log.Info().Uint("port", cfg.Port).Msg("GeoIP server listening")
	if err := http.ListenAndServe(fmt.Sprintf(":%d", cfg.Port), nil); err != nil {
		log.Fatal().Err(err).Msg("Server failed")
	}
}

func addHandlers(db *geoip2.Reader, ipHeader string, excludeSubnets []*net.IPNet) {
	http.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		handleRequest(w, r, db, ipHeader, excludeSubnets)
	})

	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	http.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready"))
	})
}
