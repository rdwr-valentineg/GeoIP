package config

import (
	"errors"
	"flag"
	"net"
	"strings"
	"time"
)

type config struct {
	DbPath               string
	Port                 uint
	IpHeader             string
	LogLevelFlag         string
	MaxMindLicenseKey    string
	MaxMindFetchInterval time.Duration
	CachePurgePeriod     time.Duration
	AllowedCodes         map[string]bool // e.g., {"US": true}
	ExcludeCIDR          []*net.IPNet    // e.g., {"10.0.0.0/8", "192.168.0.0/16"}

}

var Config *config

func InitConfig() error {
	if Config != nil {
		return nil // Already initialized
	}

	port := flag.Uint("port", 8080, "Port to listen on")
	excludeCIDR := flag.String("exclude", "192.168.0.0/16,10.0.0.0/8,172.16.0.0/12,127.0.0.0/8,::1/128", "Comma-separated CIDRs to exclude")
	allowedCountryList := flag.String("allow", "US", "Comma-separated list of ISO country codes to allow")
	ipHeader := flag.String("ip-header", "X-Forwarded-For", "Header to extract real IP")
	logLevelFlag := flag.String("log-level", "info", "Log level (none, error, info, debug)")
	dbPath := flag.String("db", "", "Path to MaxMind GeoIP2 DB")
	maxMindLicenseKey := flag.String("maxmind-license-key", "", "MaxMind license key for GeoIP2 DB updates")
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

	Config = &config{
		DbPath:               *dbPath,
		Port:                 *port,
		ExcludeCIDR:          excludeSubnets,
		AllowedCodes:         allowedMap,
		IpHeader:             *ipHeader,
		LogLevelFlag:         *logLevelFlag,
		CachePurgePeriod:     *cachePurgePeriod,
		MaxMindLicenseKey:    *maxMindLicenseKey,
		MaxMindFetchInterval: *maxMindFetchInterval,
	}

	return Config.Validate()
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

	if c.MaxMindLicenseKey != "" && c.MaxMindFetchInterval <= 0 {
		return errors.New("maxmind fetch interval must be greater than zero")
	}

	return nil
}
