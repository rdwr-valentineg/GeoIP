package db

import "github.com/oschwald/maxminddb-golang"

// GeoIPSource abstracts a GeoIP database source.
type GeoIPSource interface {
	Start() error
	Stop() error
	GetReader() *maxminddb.Reader
	IsReady() bool
	Reload() error
}
