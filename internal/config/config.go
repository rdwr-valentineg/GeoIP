package config

import (
	"errors"
	"flag"
	"time"
)

type Config struct {
	DbPath             string
	Port               uint
	ExcludeCIDR        string
	AllowedCountryList string
	IpHeader           string
	LogLevelFlag       string
	CachePurgePeriod   time.Duration
}

func InitConfig() (*Config, error) {
	dbPath := flag.String("db", "/mmdb/GeoLite2-Country.mmdb", "Path to MaxMind GeoIP2 DB")
	port := flag.Uint("port", 8080, "Port to listen on")
	excludeCIDR := flag.String("exclude", "192.168.0.0/16,10.0.0.0/8,172.16.0.0/12,127.0.0.0/8,::1/128", "Comma-separated CIDRs to exclude")
	allowedCountryList := flag.String("allow", "US", "Comma-separated list of ISO country codes to allow")
	ipHeader := flag.String("ip-header", "X-Forwarded-For", "Header to extract real IP")
	logLevelFlag := flag.String("log-level", "info", "Log level (none, error, info, debug)")
	cachePurgePeriod := flag.Duration("purge-interval", 2*time.Minute, "Interval for clearing the cache")

	flag.Parse()

	config := &Config{
		DbPath:             *dbPath,
		Port:               *port,
		ExcludeCIDR:        *excludeCIDR,
		AllowedCountryList: *allowedCountryList,
		IpHeader:           *ipHeader,
		LogLevelFlag:       *logLevelFlag,
		CachePurgePeriod:   *cachePurgePeriod,
	}
	err := config.Validate()
	return config, err
}

func (c *Config) Validate() error {
	if c.DbPath == "" {
		return errors.New("database path cannot be empty")
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
	return nil
}
