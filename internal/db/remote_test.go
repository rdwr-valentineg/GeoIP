package db

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rdwr-valentineg/GeoIP/internal/metrics"
)

// Test helpers and fixtures
func init() {
	// Initialize metrics for tests
	metrics.InitMetrics()
}

type (
	testServer struct {
		server        *httptest.Server
		client        *http.Client
		responses     []testResponse
		responseIndex int
		mutex         sync.Mutex
	}

	testResponse struct {
		statusCode int
		body       []byte
		headers    map[string]string
	}
	mockClient struct {
		err error
		res *http.Response
	}

	mockGeoIPReader struct {
		lookup func(ip net.IP, record any) error
		close  func() error
	}
)

func (m mockGeoIPReader) Lookup(ip net.IP, record any) error {
	return m.lookup(ip, record)
}
func (m mockGeoIPReader) Close() error {
	return m.close()
}
func (m *mockClient) Do(_ *http.Request) (*http.Response, error) {
	return m.res, m.err
}

func newTestServer(responses ...testResponse) *testServer {
	ts := &testServer{
		responses: responses,
	}

	ts.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ts.mutex.Lock()
		defer ts.mutex.Unlock()

		if ts.responseIndex >= len(ts.responses) {
			ts.responseIndex = len(ts.responses) - 1 // Use last response
		}

		resp := ts.responses[ts.responseIndex]
		ts.responseIndex++

		// Set headers
		for key, value := range resp.headers {
			w.Header().Set(key, value)
		}

		w.WriteHeader(resp.statusCode)
		if resp.body != nil {
			w.Write(resp.body)
		}
	}))
	ts.client = ts.server.Client()

	return ts
}

func (ts *testServer) close() {
	ts.server.Close()
}

func newValidMMDBArchive(t *testing.T) []byte {
	t.Helper()
	mockDB := mustMockValidMMDB(t)
	arch, err := CreateTarGz(mockDB, "GeoLite2-Country.mmdb")
	if err != nil {
		t.Fatalf("failed to create valid archive: %v", err)
	}
	return arch
}

func newTestRemoteFetcher(client HTTPClient, inMemory bool, dbPath string) *RemoteFetcher {
	return &RemoteFetcher{
		BasicAuth:  "Basic test-auth",
		DBPath:     dbPath,
		Interval:   time.Hour,
		Client:     client,
		URL:        maxmindBaseURL, // Use the global test URL
		inMemory:   inMemory,
		timeout:    30 * time.Second,
		maxRetries: 3,
	}
}

func TestNewRemoteFetcher(t *testing.T) {
	cfg := Config{
		AccountID:  "test-account",
		LicenseKey: "test-license",
		DBPath:     "/tmp/test.mmdb",
		Interval:   time.Hour,
	}

	rf := NewRemoteFetcher(cfg)
	if rf == nil {
		t.Fatal("expected non-nil RemoteFetcher")
	}
	if rf.BasicAuth == "" {
		t.Error("expected BasicAuth to be set")
	}
	if rf.DBPath != "/tmp/test.mmdb" {
		t.Errorf("expected DBPath to be '/tmp/test.mmdb', got %s", rf.DBPath)
	}
	if rf.Interval != time.Hour {
		t.Errorf("expected Interval to be 1 hour, got %v", rf.Interval)
	}
	if rf.Client == nil {
		t.Error("expected non-nil HTTP client")
	}
	if rf.inMemory {
		t.Error("expected inMemory to be false for file-based config")
	}
}

func TestNewRemoteFetcher_InMemory(t *testing.T) {
	cfg := Config{
		AccountID:  "test-account",
		LicenseKey: "test-license",
		DBPath:     "", // Empty path means in-memory
		Interval:   time.Hour,
	}

	rf := NewRemoteFetcher(cfg)
	if !rf.inMemory {
		t.Error("expected inMemory to be true for empty DBPath")
	}
}

func TestRemoteFetcher_Start(t *testing.T) {
	cfg := Config{
		AccountID:  "test-account",
		LicenseKey: "test-license",
		DBPath:     "",
		Interval:   time.Hour,
	}
	rf := NewRemoteFetcher(cfg)

	if err := rf.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if rf.done == nil {
		t.Fatal("expected done channel to be initialized")
	}
	if err := rf.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestRemoteFetcher_LoadsToMemory(t *testing.T) {
	archive := newValidMMDBArchive(t)
	server := newTestServer(testResponse{
		statusCode: http.StatusOK,
		body:       archive,
	})
	defer server.close()

	remote := newTestRemoteFetcher(server.client, true, "")
	remote.URL = server.server.URL

	if err := remote.fetch(); err != nil {
		t.Fatalf("fetch failed: %v", err)
	}

	if !remote.IsReady() {
		t.Error("remote should be ready after fetch")
	}
	if remote.GetReader() == nil {
		t.Error("remote should have a reader after fetch")
	}
}

// Test individual fetch method components
func TestRemoteFetcher_downloadArchive(t *testing.T) {
	archive := newValidMMDBArchive(t)
	tests := []struct {
		name        string
		server      *testServer
		expectedErr string
		rf          *RemoteFetcher
	}{
		{
			name: "Success",
			server: newTestServer(testResponse{
				statusCode: http.StatusOK,
				body:       archive,
			}),
		}, {
			name: "Forbidden response",
			server: newTestServer(testResponse{
				statusCode: http.StatusForbidden,
				body:       []byte("forbidden"),
			}),
			expectedErr: "bad response: 403 Forbidden",
		}, {
			name: "Client Error",
			server: newTestServer(testResponse{
				statusCode: http.StatusInternalServerError,
				body:       []byte("internal server error"),
			}),
			rf: &RemoteFetcher{
				Client: &mockClient{
					err: fmt.Errorf("simulated error"),
				},
			},
			expectedErr: "failed to fetch data: simulated error",
		}, {
			name: "URL parse error",
			server: newTestServer(testResponse{
				statusCode: http.StatusOK,
				body:       archive,
			}),
			rf: &RemoteFetcher{
				Client: &mockClient{},
				URL:    "http://some-bad-url\n/path", // This will cause an error in NewRequestWithContext
			},
			expectedErr: "failed to create request: parse",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				tc.server.close()
			}()
			if tc.rf == nil {
				tc.rf = newTestRemoteFetcher(tc.server.client, true, "")
			}
			if tc.rf.URL == maxmindBaseURL {
				tc.rf.URL = tc.server.server.URL
			}

			resp, err := tc.rf.downloadArchive(context.Background())
			if tc.expectedErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.expectedErr) {
					t.Fatalf("downloadArchive expected err: %s, got: %v", tc.expectedErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("downloadArchive failed: %v", err)
			}
			defer func() {
				resp.Body.Close()
			}()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("expected status 200, got %d", resp.StatusCode)
			}
		})
	}
}

func TestRemoteFetcher_downloadAndExtractDB_Success(t *testing.T) {
	archive := newValidMMDBArchive(t)
	server := newTestServer(testResponse{
		statusCode: http.StatusOK,
		body:       archive,
	})
	defer server.close()

	rf := newTestRemoteFetcher(server.client, true, "")
	rf.URL = server.server.URL

	data, size, err := rf.downloadAndExtractDB(context.Background())
	if err != nil {
		t.Fatalf("downloadAndExtractDB failed: %v", err)
	}

	if size <= 0 {
		t.Error("expected positive size")
	}
	if data == nil {
		t.Error("expected non-nil data reader")
	}
}

func TestRemoteFetcher_createInMemoryReader(t *testing.T) {
	mockDB := mustMockValidMMDB(t)
	rf := newTestRemoteFetcher(nil, true, "")
	tests := []struct {
		name        string
		db          []byte
		expectedErr string
		size        int64
	}{
		{
			name: "Valid MMDB",
			db:   mockDB,
			size: int64(len(mockDB)),
		}, {
			name:        "Invalid Reader",
			db:          []byte("invalid mmdb"),
			expectedErr: "invalid MaxMind DB file",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reader, err := rf.createInMemoryReader(tc.db)
			if tc.expectedErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.expectedErr) {
					t.Fatalf("createInMemoryReader expected err: %s, got: %v", tc.expectedErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("createInMemoryReader failed: %v", err)
			}
			defer reader.Close()
			if reader == nil {
				t.Error("expected non-nil reader")
			}
		})
	}
}

func TestRemoteFetcher_createFileReader_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping file reader test in short mode due to Windows file locking issues")
	}

	mockDB := mustMockValidMMDB(t)
	tempDir, err := os.MkdirTemp("", "remote_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.mmdb")
	rf := newTestRemoteFetcher(nil, false, dbPath)

	reader, err := rf.createFileReader(mockDB, int64(len(mockDB)))
	if err != nil {
		// On Windows, we might get file locking issues, so just check the error type
		if strings.Contains(err.Error(), "process cannot access the file") {
			t.Skip("Skipping due to Windows file locking issue")
		}
		t.Fatalf("createFileReader failed: %v", err)
	}

	// Close reader before checking file to avoid file locking issues
	reader.Close()

	// Verify file was created
	if _, err := os.Stat(dbPath); err != nil {
		t.Error("expected database file to exist")
	}
}
func TestRemoteFetcher_fetch_InMemory_Success(t *testing.T) {
	archive := newValidMMDBArchive(t)
	server := newTestServer(testResponse{
		statusCode: http.StatusOK,
		body:       archive,
	})

	defer server.close()

	rf := newTestRemoteFetcher(server.client, true, "")
	rf.URL = server.server.URL

	err := rf.fetch()
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if !rf.IsReady() {
		t.Error("expected ready after fetch")
	}
	if rf.GetReader() == nil {
		t.Error("expected reader after fetch")
	}
}

func TestRemoteFetcher_fetch_InMemory_BadStatus(t *testing.T) {
	server := newTestServer(testResponse{
		statusCode: http.StatusForbidden,
		body:       []byte("fail"),
	})
	defer server.close()

	rf := newTestRemoteFetcher(server.client, true, "")
	rf.URL = server.server.URL

	err := rf.fetch()
	if err == nil || !strings.Contains(err.Error(), "bad response") {
		t.Fatalf("expected bad response error, got %v", err)
	}
}

func TestRemoteFetcher_fetch_InMemory_InvalidGzip(t *testing.T) {
	server := newTestServer(testResponse{
		statusCode: http.StatusOK,
		body:       []byte("not a gzip"),
	})

	defer server.close()

	rf := newTestRemoteFetcher(server.client, true, "")
	rf.URL = server.server.URL

	err := rf.fetch()
	if err == nil || !strings.Contains(err.Error(), "failed to create gzip reader") {
		t.Fatalf("expected gzip error, got %v", err)
	}
}

func TestRemoteFetcher_fetch_InMemory_MissingFileInTar(t *testing.T) {
	arch, err := CreateTarGz([]byte("irrelevant"), "not-mmdb.txt")
	if err != nil {
		t.Fatal(err)
	}
	server := newTestServer(testResponse{
		statusCode: http.StatusOK,
		body:       arch,
	})

	defer server.close()

	rf := newTestRemoteFetcher(server.client, true, "")
	rf.URL = server.server.URL

	err = rf.fetch()
	if err == nil || !strings.Contains(err.Error(), "failed to extract GeoLite2-Country.mmdb from tar") {
		t.Fatalf("expected extract error, got %v", err)
	}
}

func TestRemoteFetcher_fetch_InMemory_InvalidMMDB(t *testing.T) {
	arch, err := CreateTarGz([]byte("not a mmdb"), "GeoLite2-Country.mmdb")
	if err != nil {
		t.Fatal(err)
	}
	server := newTestServer(testResponse{
		statusCode: http.StatusOK,
		body:       arch,
	})

	defer server.close()

	rf := newTestRemoteFetcher(server.client, true, "")
	rf.URL = server.server.URL

	err = rf.fetch()
	if err == nil || !strings.Contains(err.Error(), "failed to create maxmind reader from bytes") {
		t.Fatalf("expected mmdb error, got %v", err)
	}
}

func TestRemoteFetcher_fetch_SizeLimit(t *testing.T) {
	// Create a very large mock database to test size limits
	largeMockDB := make([]byte, maxDBSize+1)
	arch, err := CreateTarGz(largeMockDB, "GeoLite2-Country.mmdb")
	if err != nil {
		t.Fatal(err)
	}

	server := newTestServer(testResponse{
		statusCode: http.StatusOK,
		body:       arch,
	})

	defer server.close()

	rf := newTestRemoteFetcher(server.client, true, "")
	rf.URL = server.server.URL

	err = rf.fetch()
	if err == nil || !strings.Contains(err.Error(), "database too large") {
		t.Fatalf("expected size limit error, got %v", err)
	}
}

func TestRemoteFetcher(t *testing.T) {
	archive := newValidMMDBArchive(t)
	tests := []struct {
		name        string
		server      *testServer
		expectedErr string
	}{
		{
			name: "Immediate Success",
			server: newTestServer(testResponse{
				statusCode: http.StatusOK,
				body:       archive,
			}),
		}, {
			name: "Retry Success",
			server: newTestServer(
				testResponse{statusCode: http.StatusInternalServerError, body: []byte("error")},
				testResponse{statusCode: http.StatusInternalServerError, body: []byte("error")},
				testResponse{statusCode: http.StatusOK, body: archive},
			),
		}, {
			name:        "Retry Failure",
			expectedErr: "max retries exceeded",
			server: newTestServer(
				testResponse{statusCode: http.StatusInternalServerError, body: []byte("error")},
				testResponse{statusCode: http.StatusInternalServerError, body: []byte("error")},
				testResponse{statusCode: http.StatusInternalServerError, body: []byte("error")},
				testResponse{statusCode: http.StatusInternalServerError, body: []byte("error")},
			),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {

			defer tc.server.close()
			rf := newTestRemoteFetcher(tc.server.client, true, "")

			rf.URL = tc.server.server.URL // Use the test server URL
			// For this test, we still want fast execution
			err := rf.fetchWithRetry()
			if tc.expectedErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.expectedErr) {
					t.Errorf("expected error '%s', got %+v", tc.expectedErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("fetchWithRetry failed: %v", err)
			}
		})
	}
}

func TestRemoteFetcher_fetchWithRetry_RealTiming(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping timing test in short mode")
	}

	archive := newValidMMDBArchive(t)
	server := newTestServer(
		testResponse{statusCode: http.StatusInternalServerError, body: []byte("error")},
		testResponse{statusCode: http.StatusOK, body: archive},
	)

	defer server.close()

	rf := newTestRemoteFetcher(server.client, true, "")
	rf.URL = server.server.URL

	rf.BaseBackoff = time.Second // Use real sleep for this test

	start := time.Now()
	err := rf.fetchWithRetry()
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("fetchWithRetry should succeed after retries: %v", err)
	}

	// Should take at least 1 second (first retry delay)
	if duration < time.Second {
		t.Errorf("expected at least 1 second delay, got %v", duration)
	}
}

func TestRemoteFetcher_Reload(t *testing.T) {
	archive := newValidMMDBArchive(t)
	server := newTestServer(testResponse{
		statusCode: http.StatusOK,
		body:       archive,
	})

	defer server.close()

	rf := newTestRemoteFetcher(server.client, true, "")
	rf.URL = server.server.URL

	err := rf.Reload()
	if err != nil {
		t.Fatalf("Reload failed: %v", err)
	}
	if !rf.IsReady() {
		t.Error("expected ready after reload")
	}
}

func TestRemoteFetcher_IsReady(t *testing.T) {
	rf := newTestRemoteFetcher(nil, true, "")

	if rf.IsReady() {
		t.Error("expected not ready before fetch")
	}

	// Test with ready flag set but no reader
	rf.ready = true
	if rf.IsReady() {
		t.Error("expected not ready without reader")
	}
}

func TestRemoteFetcher_GetReader(t *testing.T) {
	// After setting up a valid reader through fetch
	archive := newValidMMDBArchive(t)
	server := newTestServer(testResponse{
		statusCode: http.StatusOK,
		body:       archive,
	})

	defer server.close()

	rf := newTestRemoteFetcher(server.client, true, "")
	rf.URL = server.server.URL
	// Initially not ready, so IsReady should be false
	if rf.IsReady() {
		t.Error("expected not ready before fetch")
	}

	if err := rf.fetch(); err != nil {
		t.Fatalf("fetch failed: %v", err)
	}

	if !rf.IsReady() {
		t.Error("expected ready after successful fetch")
	}

	reader := rf.GetReader()
	if reader == nil {
		t.Error("expected non-nil reader after successful fetch")
	}

	// Test that we can actually use the reader
	var result interface{}
	if err := reader.Lookup(net.ParseIP("1.2.3.4"), &result); err != nil {
		t.Errorf("lookup failed: %v", err)
	}
}

// Integration test with actual fetch and reader
func TestRemoteFetcher_Integration_Success(t *testing.T) {
	archive := newValidMMDBArchive(t)
	server := newTestServer(testResponse{
		statusCode: http.StatusOK,
		body:       archive,
	})

	defer server.close()
	rf := newTestRemoteFetcher(server.client, true, "")
	rf.URL = server.server.URL

	// Initial state - reader should be nil initially
	if rf.IsReady() {
		t.Error("expected not ready initially")
	}

	// After fetch
	if err := rf.fetch(); err != nil {
		t.Fatalf("fetch failed: %v", err)
	}

	if !rf.IsReady() {
		t.Error("expected ready after fetch")
	}
	if rf.GetReader() == nil {
		t.Error("expected reader after fetch")
	}

	// Test reader functionality
	reader := rf.GetReader()
	var result interface{}
	if err := reader.Lookup(net.ParseIP("1.2.3.4"), &result); err != nil {
		t.Errorf("lookup failed: %v", err)
	}
}

func TestRemoteFetcher_periodicFetch(t *testing.T) {
	tests := []struct {
		name       string
		server     *testServer
		validation func(*testing.T, float64)
	}{
		{
			name: "Valid MMDB Archive",
			server: newTestServer(testResponse{
				statusCode: http.StatusOK,
				body:       newValidMMDBArchive(t),
			}),
			validation: func(t *testing.T, initMetric float64) {
				metric, err := metrics.FetchAttemptsTotal.GetMetricWithLabelValues("maxmind")
				if err != nil {
					t.Fatalf("failed to get metric: %v", err)
				}
				currMetric := testutil.ToFloat64(metric)
				if initMetric >= currMetric {
					t.Errorf("init: %f, curr: %f - expected fetch attempts to increase", initMetric, currMetric)
				}
			},
		}, {
			name: "Multiple Retries",
			server: newTestServer(
				testResponse{statusCode: http.StatusInternalServerError, body: []byte("error")},
				testResponse{statusCode: http.StatusInternalServerError, body: []byte("error")},
				testResponse{statusCode: http.StatusOK, body: newValidMMDBArchive(t)},
			),
			validation: func(t *testing.T, initMetric float64) {
				metric, err := metrics.FetchAttemptsTotal.GetMetricWithLabelValues("maxmind")
				if err != nil {
					t.Fatalf("failed to get metric: %v", err)
				}
				currMetric := testutil.ToFloat64(metric)
				if initMetric >= currMetric {
					t.Errorf("init: %f, curr: %f - expected fetch attempts to increase after retries", initMetric, currMetric)
				}
			},
		}, {
			name: "Max Retries Exceeded",
			server: newTestServer(
				testResponse{statusCode: http.StatusInternalServerError, body: []byte("error")},
				testResponse{statusCode: http.StatusInternalServerError, body: []byte("error")},
				testResponse{statusCode: http.StatusInternalServerError, body: []byte("error")},
				testResponse{statusCode: http.StatusInternalServerError, body: []byte("error")},
			),
			validation: func(t *testing.T, initMetric float64) {
				metric, err := metrics.FetchAttemptsTotal.GetMetricWithLabelValues("maxmind")
				if err != nil {
					t.Fatalf("failed to get metric: %v", err)
				}
				currMetric := testutil.ToFloat64(metric)
				if initMetric >= currMetric {
					t.Errorf("init: %f, curr: %f - expected fetch attempts to increase after max retries", initMetric, currMetric)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				tc.server.close()
			}()
			rf := newTestRemoteFetcher(tc.server.client, true, "")
			rf.URL = tc.server.server.URL
			rf.Interval = 10 * time.Millisecond // fast ticker for test

			rf.done = make(chan struct{})
			metric, err := metrics.FetchAttemptsTotal.GetMetricWithLabelValues("maxmind")
			if err != nil {
				t.Fatalf("failed to get metric: %v", err)
			}
			initMetric := testutil.ToFloat64(metric)

			go rf.periodicFetch()
			time.Sleep(50 * time.Millisecond)
			close(rf.done)
			time.Sleep(20 * time.Millisecond) // allow goroutine to exit
			tc.validation(t, initMetric)
		})
	}
}

func TestUpdateReaderState(t *testing.T) {
	srv := newTestServer(testResponse{
		statusCode: http.StatusOK,
		body:       newValidMMDBArchive(t),
	})
	tests := []struct {
		name        string
		reader      *mockGeoIPReader
		rf          *RemoteFetcher
		expectedErr string
	}{
		{
			name: "initial close error",
			reader: &mockGeoIPReader{
				lookup: func(ip net.IP, record any) error {
					return nil
				},
				close: func() error {
					return fmt.Errorf("mock close error")
				},
			},
			rf: &RemoteFetcher{
				BasicAuth: "Basic test-auth",
				DBPath:    "",
				Interval:  time.Hour,
				Client:    srv.client,
				URL:       srv.server.URL,
				inMemory:  true,
				reader: &mockGeoIPReader{
					close: func() error {
						return fmt.Errorf("mock close error")
					},
				},
			},
			expectedErr: "", //Currently only logs the error, doesn't return it
		}, {
			name: "Lookup error",
			reader: &mockGeoIPReader{
				lookup: func(ip net.IP, record any) error {
					return fmt.Errorf("mock lookup error")
				},
				close: func() error {
					return nil
				},
			},
			rf: &RemoteFetcher{
				BasicAuth: "Basic test-auth",
				DBPath:    "",
				Interval:  time.Hour,
				Client:    srv.client,
				URL:       srv.server.URL,
				inMemory:  true,
				reader: &mockGeoIPReader{
					close: func() error {
						return fmt.Errorf("mock close error")
					},
				},
			},
			expectedErr: "database validation failed",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.rf.updateReaderState(tc.reader)
			if tc.expectedErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.expectedErr) {
					t.Errorf("expected error '%s', got %v", tc.expectedErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("updateReaderState failed: %v", err)
			}
		})
	}
}

// Test helpers for creating archives and mock data
func mustMockValidMMDB(t *testing.T) []byte {
	t.Helper()
	return GenerateValidMockMMDB()
}

func GenerateValidMockMMDB() []byte {
	addNet := func(writer *mmdbwriter.Tree, ip string, mask int, isoCode string) error {
		net := &net.IPNet{
			IP:   net.ParseIP(ip),
			Mask: net.CIDRMask(mask, 32),
		}
		return writer.Insert(net, mmdbtype.Map{
			"country": mmdbtype.Map{
				"iso_code": mmdbtype.String(isoCode),
			},
		})
	}

	writer, err := mmdbwriter.New(
		mmdbwriter.Options{
			DatabaseType: "GeoLite2-Country",
		},
	)
	if err != nil {
		log.Fatalf("failed to create mmdbwriter: %v", err)
	}

	if err := addNet(writer, "1.2.3.0", 24, "US"); err != nil {
		log.Fatalf("failed to insert US: %v", err)
	}
	if err := addNet(writer, "2.3.4.0", 24, "RU"); err != nil {
		log.Fatalf("failed to insert RU: %v", err)
	}

	var buf bytes.Buffer
	if _, err := writer.WriteTo(&buf); err != nil {
		log.Fatalf("failed to write mmdb to buffer: %v", err)
	}

	return buf.Bytes()
}

func CreateTarGz(data []byte, filename string) ([]byte, error) {
	var buf bytes.Buffer

	gzw := gzip.NewWriter(&buf)
	defer gzw.Close()

	tw := tar.NewWriter(gzw)
	defer tw.Close()

	hdr := &tar.Header{
		Name:    filename,
		Mode:    0644,
		Size:    int64(len(data)),
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return nil, err
	}

	if _, err := tw.Write(data); err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gzw.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
