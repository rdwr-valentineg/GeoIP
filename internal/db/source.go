package db

import (
	"net"
)

// GeoIPSource abstracts a GeoIP database source.
type GeoIPSource interface {
	Start() error
	Stop() error
	GetReader() ReaderInterface
	IsReady() bool
	Reload() error
}

type ReaderInterface interface {
	Lookup(ip net.IP, result interface{}) error
	Close() error
}
