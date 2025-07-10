package db

import (
	"net"
)

// GeoIPSource abstracts a GeoIP database source.
type GeoIPSource interface {
	Fetcher
	DatabaseProvider
}

type Fetcher interface {
	Start() error
	Stop() error
	IsReady() bool
	Reload() error
}

type DatabaseProvider interface {
	GetReader() ReaderInterface
}

type ReaderInterface interface {
	Lookup(ip net.IP, result interface{}) error
	Close() error
}
