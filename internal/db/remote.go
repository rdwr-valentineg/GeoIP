package db

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
	"strings"
        "encoding/base64"
	"github.com/rs/zerolog/log"
        "archive/tar"
	"compress/gzip"

	"github.com/oschwald/maxminddb-golang"
	"github.com/rdwr-valentineg/GeoIP/internal/config"
)

type RemoteFetcher struct {
	BasicAuth string
	DBPath     string // optional
	Interval   time.Duration
	Client     *http.Client
	mutex      sync.RWMutex
	reader     *maxminddb.Reader
	ready      bool
	done       chan struct{}
	inMemory   bool
}

var maxmindBaseURL = "https://download.maxmind.com/geoip/databases/GeoLite2-Country/download?suffix=tar.gz"

func NewRemoteFetcher() *RemoteFetcher {
	auth := fmt.Sprintf("%s:%s", config.GetMaxMindAccountId(), config.GetMaxMindLicenseKey())
	b64Auth := base64.StdEncoding.EncodeToString([]byte(auth))

	return &RemoteFetcher{
		BasicAuth: "Basic "+b64Auth,
		DBPath:     config.GetDbPath(),
		Interval:   config.GetMaxMindFetchInterval(),
		Client:     &http.Client{Timeout: 30 * time.Second},
		inMemory:   config.GetDbPath() == "",
	}
}

func (r *RemoteFetcher) Start() error {
	if r.BasicAuth == "" {
		return fmt.Errorf("auth info is required")
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
	req, err := http.NewRequest("GET", maxmindBaseURL, nil)
	if err != nil {
		panic(err)
	}

	// Add Basic Auth header
	req.Header.Add("Authorization", r.BasicAuth)
resp, err := r.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad response: %s", resp.Status)
	}

	gzr, err := gzip.NewReader(resp.Body)
	if err != nil {
		panic(err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	data,size , err := extractFileFromTar(tr, "GeoLite2-Country.mmdb")
	if err != nil {
		panic(err)
	}

        buf := make([]byte, size)
	var reader *maxminddb.Reader
	if r.inMemory {
		_, err := io.ReadFull(data, buf)
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
		if _, err := io.CopyN(out, data, size); err != nil {
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

func extractFileFromTar(tr *tar.Reader, target string) (io.Reader, int64, error) {
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, err
		}

		if header.Typeflag != tar.TypeReg {
			continue
		}

		log.Info().Str("name", header.Name).Msg("found in tar")

		if strings.Contains( header.Name, target) {
			// Wrap in a LimitedReader to avoid reading beyond the file size
			return io.LimitReader(tr, header.Size), header.Size, nil
		}
	}
	return nil, 0, fmt.Errorf("file %s not found in archive", target)
}
