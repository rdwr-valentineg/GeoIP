package db

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/oschwald/maxminddb-golang"
	"github.com/pkg/errors"
	"github.com/rdwr-valentineg/GeoIP/internal/metrics"
	"github.com/rdwr-valentineg/GeoIP/internal/utils"
	"github.com/rs/zerolog/log"
)

type (
	RemoteFetcher struct {
		BasicAuth   string
		DBPath      string // optional
		Interval    time.Duration
		Client      HTTPClient
		URL         string
		BaseBackoff time.Duration
		timeout     time.Duration
		mutex       sync.RWMutex
		reader      ReaderInterface
		ready       bool
		done        chan struct{}
		inMemory    bool
		maxRetries  int
	}

	HTTPClient interface {
		Do(req *http.Request) (*http.Response, error)
	}

	FileSystem interface {
		Create(name string) (io.WriteCloser, error)
		Rename(oldpath, newpath string) error
	}
	Config struct {
		AccountID   string
		LicenseKey  string
		DBPath      string
		Interval    time.Duration
		Timeout     time.Duration
		MaxRetries  int
		BaseBackoff time.Duration
	}
)

const (
	maxDBSize      = 500 * 1024 * 1024 // 500MB limit
	maxmindBaseURL = "https://download.maxmind.com/geoip/databases/GeoLite2-Country/download?suffix=tar.gz"
)

func NewRemoteFetcher(cfg Config) *RemoteFetcher {
	auth := fmt.Sprintf("%s:%s", cfg.AccountID, cfg.LicenseKey)
	b64Auth := base64.StdEncoding.EncodeToString([]byte(auth))
	dbPath := cfg.DBPath
	return &RemoteFetcher{
		BasicAuth:   "Basic " + b64Auth,
		DBPath:      dbPath,
		Interval:    cfg.Interval,
		URL:         maxmindBaseURL, // Use configurable URL
		BaseBackoff: time.Second,
		Client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     30 * time.Second,
			},
		},
		inMemory:   dbPath == "",
		timeout:    cfg.Timeout,
		maxRetries: cfg.MaxRetries,
	}
}

func (r *RemoteFetcher) Start() error {
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

func (r *RemoteFetcher) GetReader() ReaderInterface {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return r.reader
}

func (r *RemoteFetcher) Reload() error {
	return r.fetchWithRetry()
}

func (r *RemoteFetcher) periodicFetch() {
	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()

	if err := r.fetchWithRetry(); err != nil {
		log.Info().Err(err).Msg("fetch error!")
	}
	for {
		select {
		case <-ticker.C:
			if err := r.fetchWithRetry(); err != nil {
				log.Info().Err(err).Msg("fetch error!")
			}
		case <-r.done:
			return
		}
	}
}

func (r *RemoteFetcher) fetch() error {
	// Track fetch attempt
	metrics.FetchAttemptsTotal.WithLabelValues("maxmind").Inc()
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()

	// Download and extract database
	data, size, err := r.downloadAndExtractDB(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to download and extract DB")
		metrics.FetchErrorsTotal.WithLabelValues("download_and_extract").Inc()
		return err
	}

	// Validate size
	if size > maxDBSize {
		metrics.FetchErrorsTotal.WithLabelValues("size_validation").Inc()
		err = fmt.Errorf("database too large: %d bytes", size)
		log.Error().Err(err).Msg("Failed to download and extract DB max size exceeded")
		return err
	}

	// Create reader from data
	reader, err := r.createReader(data, size)
	if err != nil {
		metrics.FetchErrorsTotal.WithLabelValues("reader_creation").Inc()
		log.Error().Err(err).Msg("Failed to create reader")
		return err
	}

	// Update the fetcher state
	if err := r.updateReaderState(reader); err != nil {
		metrics.FetchErrorsTotal.WithLabelValues("reader_state_update").Inc()
		log.Error().Err(err).Msg("Failed to update reader state")
		return err
	}
	log.Debug().
		Int64("size_bytes", size).
		Msg("Database fetch completed successfully")
	return nil
}

func (r *RemoteFetcher) downloadAndExtractDB(ctx context.Context) ([]byte, int64, error) {
	resp, err := r.downloadArchive(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	gzr, err := gzip.NewReader(resp.Body)
	if err != nil {
		metrics.FetchErrorsTotal.WithLabelValues("gzip_decompression").Inc()
		return nil, 0, errors.Wrap(err, "failed to create gzip reader")
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	data, size, err := utils.ExtractFileFromTar(tr, "GeoLite2-Country.mmdb")
	if err != nil {
		metrics.FetchErrorsTotal.WithLabelValues("tar_extraction").Inc()
		return nil, 0, errors.Wrap(err, "failed to extract GeoLite2-Country.mmdb from tar")
	}

	// Buffer the data in memory before closing resp.Body
	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, data); err != nil {
		return nil, 0, errors.Wrap(err, "failed to buffer mmdb data")
	}

	log.Debug().
		Str("endpoint", "maxmind").
		Int64("size_bytes", size).
		Msg("Database extraction completed successfully")
	return buf.Bytes(), int64(buf.Len()), nil
}

func (r *RemoteFetcher) downloadArchive(ctx context.Context) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", r.URL, nil)
	if err != nil {
		metrics.FetchErrorsTotal.WithLabelValues("http_request_creation").Inc()
		return nil, errors.Wrap(err, "failed to create request")
	}

	// Add Basic Auth header
	req.Header.Add("Authorization", r.BasicAuth)
	resp, err := r.Client.Do(req)
	if err != nil {
		metrics.FetchErrorsTotal.WithLabelValues("http_request_execution").Inc()
		return nil, errors.Wrap(err, "failed to fetch data")
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		metrics.FetchErrorsTotal.WithLabelValues("http_status_error").Inc()
		return nil, fmt.Errorf("bad response: %s", resp.Status)
	}
	log.Debug().
		Str("endpoint", "maxmind").
		Int64("size_bytes", resp.ContentLength).
		Msg("database fetch completed successfully")
	return resp, nil
}

func (r *RemoteFetcher) createReader(data []byte, size int64) (ReaderInterface, error) {
	if r.inMemory {
		return r.createInMemoryReader(data)
	}
	return r.createFileReader(data, size)
}

func (r *RemoteFetcher) createInMemoryReader(data []byte) (ReaderInterface, error) {
	reader, err := maxminddb.FromBytes(data)
	if err != nil {
		metrics.FetchErrorsTotal.WithLabelValues("maxmind_reader_creation").Inc()
		return nil, errors.Wrap(err, "failed to create maxmind reader from bytes")
	}

	log.Debug().
		Str("endpoint", "maxmind").
		Msg("Database in memory reader created successfully")
	return reader, nil
}

func (r *RemoteFetcher) createFileReader(data []byte, size int64) (ReaderInterface, error) {
	// Write to temporary file
	out, tmpPath, err := utils.CreateTempFile(r.DBPath)
	if err != nil {
		metrics.FetchErrorsTotal.WithLabelValues("file_creation").Inc()
		return nil, err
	}
	defer out.Close()

	if _, err := io.CopyN(out, bytes.NewReader(data), size); err != nil {
		metrics.FetchErrorsTotal.WithLabelValues("file_write").Inc()
		return nil, errors.Wrap(err, "failed to copy data to temporary file")
	}

	// Create reader from temporary file
	reader, err := maxminddb.Open(tmpPath)
	if err != nil {
		metrics.FetchErrorsTotal.WithLabelValues("maxmind_reader_creation").Inc()
		return nil, errors.Wrap(err, "failed to open maxmind reader from file")
	}

	// Atomically replace the database file
	if err := utils.AtomicReplaceFile(tmpPath, r.DBPath); err != nil {
		reader.Close()
		os.Remove(tmpPath)
		metrics.FetchErrorsTotal.WithLabelValues("file_rename").Inc()
		return nil, err
	}

	log.Debug().
		Str("endpoint", "maxmind").
		Int64("size_bytes", size).
		Msg("Database file reader created successfully")
	return reader, nil
}

func (r *RemoteFetcher) updateReaderState(reader ReaderInterface) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	// Close previous reader
	if r.reader != nil {
		if err := r.reader.Close(); err != nil {
			log.Error().Err(err).Msg("failed to close previous reader")
		}
	}

	// Validate new reader
	var testResult any
	if err := reader.Lookup(net.ParseIP("8.8.8.8"), &testResult); err != nil {
		reader.Close()
		return errors.Wrap(err, "database validation failed")
	}

	// Update state
	r.reader = reader
	r.ready = true

	// Track successful fetch
	metrics.FetchSuccessTotal.Inc()

	log.Debug().
		Str("endpoint", "maxmind").
		Msg("database update completed successfully")

	return nil
}

func (r *RemoteFetcher) fetchWithRetry() error {
	var err error
	for i := range r.maxRetries {
		if err = r.fetch(); err != nil {
			log.Error().
				Err(err).
				Int("retry", i+1).
				Str("endpoint", "maxmind").
				Msg("database fetch failed")
			time.Sleep(r.BaseBackoff * time.Duration(i+1))
			continue
		}
		return nil
	}
	return errors.Wrap(err, "max retries exceeded")
}
