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

	// s := &Server{
	// 	Db: source,
	// 	Srv: &http.Server{
	// 		Addr:    ":" + config.Config.Port,
	// 		Handler: mux,
	// }

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
	addr := fmt.Sprintf(":%d", config.Config.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		log.Info().Uint("port", config.Config.Port).Msg("GeoIP server listening")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("HTTP server error")
		}
	}()

	return &Server{Srv: srv}, nil
}
