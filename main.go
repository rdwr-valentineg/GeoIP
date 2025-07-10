package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rdwr-valentineg/GeoIP/internal/config"
	"github.com/rdwr-valentineg/GeoIP/internal/db"
	"github.com/rdwr-valentineg/GeoIP/internal/metrics"
	"github.com/rdwr-valentineg/GeoIP/internal/webserver"
	"github.com/rs/zerolog/log"
)

type (
	AuthResponse struct {
		Message string `json:"message,omitempty"`
	}
)

func clearCachePeriodically(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			evicted := webserver.CacheCleanup()
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
	case config.GetMaxMindLicenseKey() != "":
		log.Debug().Msg("Using MaxMind remote fetcher")
		source = db.NewRemoteFetcher(db.Config{
			AccountID:  config.GetMaxMindAccountId(),
			LicenseKey: config.GetMaxMindLicenseKey(),
			DBPath:     config.GetDbPath(),
			Interval:   config.GetMaxMindFetchInterval(),
		})
	case config.GetDbPath() != "":
		log.Debug().Msg("Using MaxMind local fetcher")
		source = db.NewDiskLoader(config.GetDbPath())
	default:
		log.Fatal().Msg("Either --db-path or --maxmind-license-key must be provided")
	}

	if err := source.Start(); err != nil {
		log.Fatal().Err(err).Msg("Failed to start DB source")
	}
	log.Debug().Msg("DB started successfully")

	defer source.Stop()

	metrics.InitMetrics()
	clearCachePeriodically(config.GetCachePurgePeriod())
	s, err := webserver.Run(source)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to start web server")
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Info().Msg("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.Srv.Shutdown(ctx); err != nil {
		log.Err(err).Msg("Shutdown failed")
	}
	log.Info().Msg("Server gracefully stopped")
}
