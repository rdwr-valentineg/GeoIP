package db

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/oschwald/maxminddb-golang"
	"github.com/rdwr-valentineg/GeoIP/internal/config"
)

type RemoteFetcher struct {
	LicenseKey string
	DBPath     string // optional
	Interval   time.Duration
	Client     *http.Client
	mutex      sync.RWMutex
	reader     *maxminddb.Reader
	ready      bool
	done       chan struct{}
	inMemory   bool
}

var maxmindBaseURL = "https://download.maxmind.com/app/geoip_download"

func NewRemoteFetcher() *RemoteFetcher {
	cfg := config.Config
	return &RemoteFetcher{
		LicenseKey: cfg.MaxMindLicenseKey,
		DBPath:     cfg.DbPath,
		Interval:   cfg.MaxMindFetchInterval,
		Client:     &http.Client{Timeout: 30 * time.Second},
		inMemory:   cfg.DbPath == "",
	}
}

func (r *RemoteFetcher) Start() error {
	if r.LicenseKey == "" {
		return fmt.Errorf("license key is required")
	}

	r.done = make(chan struct{})
	go r.periodicFetch()
	return nil
}

func (r *RemoteFetcher) Stop() error {
	if r.done != nil {
		close(r.done)
	}
	return nil
}

func (r *RemoteFetcher) IsReady() bool {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return r.ready && r.reader != nil
}

func (r *RemoteFetcher) GetReader() *maxminddb.Reader {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return r.reader
}

func (r *RemoteFetcher) Reload() error {
	return r.fetch()
}

func (r *RemoteFetcher) periodicFetch() {
	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()

	if err := r.fetch(); err != nil {
		log.Info().Err(err).Msg("fetch error!")
	}
	for {
		select {
		case <-ticker.C:
			r.fetch()
		case <-r.done:
			return
		}
	}
}

func (r *RemoteFetcher) fetch() error {
	url := fmt.Sprintf("%s?edition_id=GeoLite2-Country&license_key=%s&suffix=mmdb", maxmindBaseURL, r.LicenseKey)
	resp, err := r.Client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad response: %s", resp.Status)
	}

	var reader *maxminddb.Reader
	if r.inMemory {
		buf, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		reader, err = maxminddb.FromBytes(buf)
		if err != nil {
			return err
		}
	} else {
		// Write to file
		tmpPath := r.DBPath + ".tmp"
		out, err := os.Create(tmpPath)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, resp.Body); err != nil {
			out.Close()
			return err
		}
		out.Close()

		reader, err = maxminddb.Open(tmpPath)
		if err != nil {
			return err
		}
		if err := os.Rename(tmpPath, r.DBPath); err != nil {
			return err
		}
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.reader != nil {
		if err := r.reader.Close(); err != nil {
			log.Error().Err(err).Msg("failed to close previous reader")
		}
	}
	r.reader = reader
	r.ready = true
	return nil
}
