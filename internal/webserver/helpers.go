package webserver

import (
	"net"
	"net/http"
	"strings"

	"github.com/rdwr-valentineg/GeoIP/internal/config"
	"github.com/rdwr-valentineg/GeoIP/internal/metrics"
	"github.com/rs/zerolog/log"
)

var (
	serveVerdict = func(w http.ResponseWriter, allowed bool, country string) {
		if allowed {
			respondAllowed(w, country)
			metrics.RequestsTotal.WithLabelValues(country, "true").Inc()
			log.Debug().Str("Country", country).Msg("allowed")
		} else {
			http.Error(w, "Forbidden", http.StatusForbidden)
			metrics.RequestsTotal.WithLabelValues(country, "false").Inc()
			log.Debug().Str("Country", country).Msg("denied")
		}
	}

	isExcluded = func(ip net.IP, excluded []*net.IPNet) bool {
		for _, subnet := range excluded {
			if subnet.Contains(ip) {
				return true
			}
		}
		return false
	}

	respondAllowed = func(w http.ResponseWriter, isoCode string) {
		w.Header().Set("X-Country", isoCode)
		w.WriteHeader(http.StatusOK)
	}

	getIPFromRequest = func(r *http.Request) net.IP {
		hdr := r.Header.Get(config.GetIpHeader())
		if hdr != "" {
			log.Debug().Str("value", hdr).Msg("ip header found")
			parts := strings.Split(hdr, ",")
			ip := strings.TrimSpace(parts[0])
			return net.ParseIP(ip)
		}
		log.Debug().Str("value", r.RemoteAddr).Msg("ip header found not found, using RemoteAddr")
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return nil
		}
		return net.ParseIP(host)
	}
)
