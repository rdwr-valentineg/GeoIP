package main

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/rdwr-valentineg/GeoIP/internal/config"
	"github.com/rdwr-valentineg/GeoIP/internal/db"
	"github.com/rdwr-valentineg/GeoIP/internal/metrics"
	"github.com/rdwr-valentineg/GeoIP/internal/webserver"
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

func main() {
	err := config.InitConfig()
	if err != nil {
		log.Fatal().Err(err).Msg("Invalid configuration")
	}

	InitLogger()

	var source db.GeoIPSource
	switch {
	case config.Config.MaxMindLicenseKey != "":
		source = db.NewRemoteFetcher()
	case config.Config.DbPath != "":
		source = db.NewDiskLoader(config.Config.DbPath)
	default:
		log.Fatal().Msg("Either --db-path or --maxmind-license-key must be provided")
	}

	if err := source.Start(); err != nil {
		log.Fatal().Err(err).Msg("Failed to start DB source")
	}
	defer source.Stop()

	metrics.InitMetrics()
	clearCachePeriodically(config.Config.CachePurgePeriod)
	if err := webserver.NewServer(source).Start(); err != nil {
		log.Fatal().Err(err).Msg("Failed to start web server")
	}
}
