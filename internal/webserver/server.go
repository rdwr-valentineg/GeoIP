package webserver

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/rdwr-valentineg/GeoIP/internal/config"
	"github.com/rdwr-valentineg/GeoIP/internal/db"
	"github.com/rs/zerolog/log"
)

type Server struct {
	Db           db.GeoIPSource
	AllowedCodes map[string]bool // e.g., {"US": true}
	AllowedCidrs []*net.IPNet    // e.g., {"10.0.0.0/8", "192.168.0.0/16"}
}

func NewServer(source db.GeoIPSource) *Server {
	allowedMap := make(map[string]bool, 0)
	for c := range strings.SplitSeq(config.Config.AllowedCountryList, ",") {
		allowedMap[strings.ToUpper(strings.TrimSpace(c))] = true
	}
	excludeSubnets := make([]*net.IPNet, 0, 10)
	for cidr := range strings.SplitSeq(config.Config.ExcludeCIDR, ",") {
		_, ipnet, err := net.ParseCIDR(strings.TrimSpace(cidr))
		if err == nil {
			excludeSubnets = append(excludeSubnets, ipnet)
		}
	}

	return &Server{
		Db:           source,
		AllowedCodes: allowedMap,
		AllowedCidrs: excludeSubnets,
	}
}

func (srv *Server) Start() error {
	log.Info().Uint("port", config.Config.Port).Msg("GeoIP server listening")
	if err := http.ListenAndServe(fmt.Sprintf(":%d", config.Config.Port), nil); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	http.HandleFunc("/auth", srv.ServeHTTP)

	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	http.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		if !srv.Db.IsReady() {
			log.Warn().Msg("GeoIP database is not ready")
			http.Error(w, "Service not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	return nil
}
