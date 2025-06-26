package config

import (
	"errors"
	"flag"
	"net"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

type config struct {
	DbPath               string
	Port                 uint
	IpHeader             string
	LogLevelFlag         string
	MaxMindLicenseKey    string
	MaxMindAccountId     string
	MaxMindFetchInterval time.Duration
	CachePurgePeriod     time.Duration
	AllowedCodes         map[string]bool
	ExcludeCIDR          []*net.IPNet
}

var cfg *config

func InitConfig() error {
	if cfg != nil {
		return nil // Already initialized
	}

	port := flag.Uint("port", 8080, "Port to listen on")
	excludeCIDR := flag.String("exclude", "192.168.0.0/16,10.0.0.0/8,172.16.0.0/12,127.0.0.0/8,::1/128", "Comma-separated CIDRs to exclude")
	allowedCountryList := flag.String("allow", "US", "Comma-separated list of ISO country codes to allow")
	ipHeader := flag.String("ip-header", "X-Forwarded-For", "Header to extract real IP")
	logLevelFlag := flag.String("log-level", "info", "Log level (none, error, info, debug)")
	dbPath := flag.String("db", "", "Path to MaxMind GeoIP2 DB")
	maxMindLicenseKey := flag.String("maxmind-license-key", "", "MaxMind license key for GeoIP2 DB updates")
	maxMindAccountId := flag.String("maxmind-account-id", "", "MaxMind account id for GeoIP2 DB updates")
	maxMindFetchInterval := flag.Duration("maxmind-fetch-interval", 24*time.Hour, "Interval for fetching MaxMind GeoIP2 DB updates")
	cachePurgePeriod := flag.Duration("purge-interval", 2*time.Minute, "Interval for clearing the cache")

	flag.Parse()

	allowedMap := make(map[string]bool, 0)
	for c := range strings.SplitSeq(*allowedCountryList, ",") {
		allowedMap[strings.ToUpper(strings.TrimSpace(c))] = true
	}
	excludeSubnets := make([]*net.IPNet, 0, 10)
	for cidr := range strings.SplitSeq(*excludeCIDR, ",") {
		_, ipnet, err := net.ParseCIDR(strings.TrimSpace(cidr))
		if err == nil {
			excludeSubnets = append(excludeSubnets, ipnet)
		}
	}

	cfg = &config{
		DbPath:               *dbPath,
		Port:                 *port,
		ExcludeCIDR:          excludeSubnets,
		AllowedCodes:         allowedMap,
		IpHeader:             *ipHeader,
		LogLevelFlag:         *logLevelFlag,
		CachePurgePeriod:     *cachePurgePeriod,
		MaxMindLicenseKey:    *maxMindLicenseKey,
		MaxMindAccountId:     *maxMindAccountId,
		MaxMindFetchInterval: *maxMindFetchInterval,
	}

	log.Debug().Any("config", cfg).Msg("Configuration initialized")
	return cfg.Validate()
}

func (c *config) Validate() error {
	if c.DbPath == "" && c.MaxMindLicenseKey == "" {
		return errors.New("both database path and Maxmind license key cannot be empty")
	}
	if c.Port <= 0 || c.Port > 65536 {
		return errors.New("invalid port value, must be between 1 and 65536")
	}

	if c.IpHeader == "" {
		return errors.New("source IP header cannot be empty")
	}
	if c.CachePurgePeriod <= 0 {
		return errors.New("cache purge interval must be greater than zero")
	}

	if c.MaxMindLicenseKey != "" && c.MaxMindAccountId == "" {
		return errors.New("when maxmind license key provided, maxmind account id is required")
	}

	if c.MaxMindLicenseKey != "" && c.MaxMindFetchInterval <= 0 {
		return errors.New("maxmind fetch interval must be greater than zero")
	}

	return nil
}

func GetDbPath() string {
	if cfg != nil {
		return cfg.DbPath
	}
	return ""
}

func GetPort() uint {
	if cfg != nil {
		return cfg.Port
	}
	return 0
}

func GetIpHeader() string {
	if cfg != nil {
		return cfg.IpHeader
	}
	return ""
}

func GetLogLevel() string {
	if cfg != nil {
		return cfg.LogLevelFlag
	}
	return ""
}

func GetMaxMindLicenseKey() string {
	if cfg != nil {
		return cfg.MaxMindLicenseKey
	}
	return ""
}

func GetMaxMindAccountId() string {
	if cfg != nil {
		return cfg.MaxMindAccountId
	}
	return ""
}

func GetMaxMindFetchInterval() time.Duration {
	if cfg != nil {
		return cfg.MaxMindFetchInterval
	}
	return time.Duration(0)
}

func GetCachePurgePeriod() time.Duration {
	if cfg != nil {
		return cfg.CachePurgePeriod
	}
	return time.Duration(0)
}

func GetAllowedCodes() map[string]bool {
	if cfg != nil {
		return cfg.AllowedCodes
	}
	return nil
}

func GetExcludeCIDR() []*net.IPNet {
	if cfg != nil {
		return cfg.ExcludeCIDR
	}
	return nil
}
