package webserver

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rdwr-valentineg/GeoIP/internal/config"
	"github.com/rdwr-valentineg/GeoIP/internal/db"
	"github.com/rs/zerolog/log"
)

type Server struct {
	Srv *http.Server
}

func Run(source db.GeoIPSource) (*Server, error) {
	mux := http.NewServeMux()

	mux.Handle("/auth", NewAuthHandler(source))

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		log.Debug().Msg("/healthz endpoint called")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		ready := source.IsReady()
		log.Debug().Bool("Ready", ready).Msg("/healthz endpoint called")
		if !ready {
			log.Warn().Msg("GeoIP database is not ready")
			http.Error(w, "Service not ready", http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	})

	mux.Handle("/metrics", promhttp.Handler())
	addr := fmt.Sprintf(":%d", config.GetPort())
	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		log.Info().Str("addr", addr).Msg("GeoIP server listening")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("HTTP server error")
		}
	}()

	return &Server{Srv: srv}, nil
}
