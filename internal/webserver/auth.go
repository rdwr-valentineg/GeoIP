package webserver

import (
	"net/http"
	"strings"
	"sync"

	"github.com/rdwr-valentineg/GeoIP/internal/config"
	"github.com/rdwr-valentineg/GeoIP/internal/db"
	"github.com/rdwr-valentineg/GeoIP/internal/metrics"
	"github.com/rs/zerolog/log"
)

type (
	AuthHandler struct {
		Db db.GeoIPSource
	}

	geoRecord struct {
		Country struct {
			ISOCode string `maxminddb:"iso_code"`
		} `maxminddb:"country"`
	}
	cacheEntry struct {
		allowed bool
		country string
	}
)

var (
	geoCache = make(map[string]cacheEntry)
	cacheMux = sync.RWMutex{}
)

func NewAuthHandler(db db.GeoIPSource) *AuthHandler {
	return &AuthHandler{
		Db: db,
	}
}

func (ah *AuthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Debug().Bool("ready", ah.Db.IsReady()).Msg("new auth request")
	if !ah.Db.IsReady() {
		http.Error(w, "GeoIP DB not ready", http.StatusServiceUnavailable)
		return
	}

	ip := getIPFromRequest(r)
	log.Debug().Str("ip", ip.String()).Msg("auth request from")
	if ip == nil {
		http.Error(w, "Unable to determine IP", http.StatusBadRequest)
		return
	}

	cacheMux.RLock()
	entry, found := geoCache[ip.String()]
	cacheMux.RUnlock()

	if found {
		log.Debug().
			Str("ip", ip.String()).
			Str("country", entry.country).
			Msg("Cache hit for")
		metrics.CacheHits.Inc()
		serveVerdict(w, entry.allowed, entry.country)
		return
	}

	if isExcluded(ip, config.Config.ExcludeCIDR) {
		log.Debug().Str("ip", ip.String()).Msg("Excluded IP allowed")
		respondAllowed(w, "LAN")
		metrics.RequestsTotal.WithLabelValues("LAN", "true").Inc()
		return
	}

	var record geoRecord
	err := ah.Db.GetReader().Lookup(ip, &record)
	if err != nil {
		http.Error(w, "GeoIP lookup failed", http.StatusInternalServerError)
		return
	}

	isoCode := strings.ToUpper(record.Country.ISOCode)
	allowed := config.Config.AllowedCodes[isoCode]

	cacheMux.Lock()
	geoCache[ip.String()] = cacheEntry{
		allowed: allowed,
		country: isoCode,
	}
	cacheMux.Unlock()
	serveVerdict(w, allowed, isoCode)
}
