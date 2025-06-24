package webserver

import (
	"net"
	"net/http"
	"strings"

	"github.com/rdwr-valentineg/GeoIP/internal/config"
	"github.com/rdwr-valentineg/GeoIP/internal/metrics"
)

func serveVerdict(w http.ResponseWriter, allowed bool, country string) {
	if allowed {
		respondAllowed(w, country)
		metrics.RequestsTotal.WithLabelValues(country, "true").Inc()
		return
	}
	http.Error(w, "Forbidden", http.StatusForbidden)
	metrics.RequestsTotal.WithLabelValues(country, "false").Inc()
	return
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

func getIPFromRequest(r *http.Request) net.IP {
	forwarded := r.Header.Get(config.Config.IpHeader)
	if forwarded != "" {
		parts := strings.Split(forwarded, ",")
		ip := strings.TrimSpace(parts[0])
		return net.ParseIP(ip)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return nil
	}
	return net.ParseIP(host)
}
