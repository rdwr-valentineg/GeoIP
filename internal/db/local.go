package db

import (
	"fmt"
	"os"
	"sync"

	"github.com/oschwald/maxminddb-golang"
)

type DiskLoader struct {
	DBPath string

	mutex  sync.RWMutex
	reader *maxminddb.Reader
	ready  bool
}

func NewDiskLoader(dbPath string) *DiskLoader {
	return &DiskLoader{
		DBPath: dbPath,
	}
}

func (d *DiskLoader) Start() error {
	return d.Reload()
}

func (d *DiskLoader) Stop() error {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if d.reader != nil {
		return d.reader.Close()
	}
	return nil
}

func (d *DiskLoader) Reload() error {
	f, err := os.Open(d.DBPath)
	if err != nil {
		return fmt.Errorf("failed to open db path: %w", err)
	}
	defer f.Close()

	reader, err := maxminddb.Open(d.DBPath)
	if err != nil {
		return err
	}

	d.mutex.Lock()
	defer d.mutex.Unlock()
	if d.reader != nil {
		_ = d.reader.Close()
	}
	d.reader = reader
	d.ready = true
	return nil
}

func (d *DiskLoader) GetReader() *maxminddb.Reader {
	d.mutex.RLock()
	defer d.mutex.RUnlock()
	return d.reader
}

func (d *DiskLoader) IsReady() bool {
	d.mutex.RLock()
	defer d.mutex.RUnlock()
	return d.ready
}
