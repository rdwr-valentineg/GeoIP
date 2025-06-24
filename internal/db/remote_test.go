package db

import (
	"bytes"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
)

func TestRemoteFetcher_LoadsToMemory(t *testing.T) {
	// Create mock valid .mmdb content
	mockDB := mustMockValidMMDB(t)

	// Setup mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mockDB)
	}))
	defer server.Close()

	// Patch URL for test
	oldURL := maxmindBaseURL
	maxmindBaseURL = server.URL
	defer func() { maxmindBaseURL = oldURL }()

	remote := &RemoteFetcher{
		LicenseKey: "dummy",
		DBPath:     "",
		Interval:   time.Hour,
		Client:     server.Client(),
		inMemory:   true,
	}

	if err := remote.fetch(); err != nil {
		t.Fatalf("fetch failed: %v", err)
	}

	if ready := remote.IsReady(); !ready {
		t.Fatalf("remote should be ready after fetch, got: %v", ready)
	}
	if reader := remote.GetReader(); reader == nil {
		t.Fatalf("remote should have a reader after fetch, got: %v", reader)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

// mustMockValidMMDB returns a minimal valid .mmdb file content
func mustMockValidMMDB(t *testing.T) []byte {
	// MaxMind readers expect valid structure, not arbitrary bytes.
	// A pre-generated valid .mmdb fixture is ideal, but for now we simulate basic usage
	t.Helper()
	// Embed a known valid file or prepare a minimal valid binary via tools (skipped here).
	// This will fail at runtime if the reader expects a real file.
	// Use a pre-generated file with maxminddb.NewFromBytes() in real scenarios.
	return GenerateMockMMDB()
}

func GenerateMockMMDB() []byte {
	addNet := func(writer *mmdbwriter.Tree, ip string, mask int, isoCode string) error {
		net := &net.IPNet{
			IP:   net.ParseIP("1.2.3.0"),
			Mask: net.CIDRMask(24, 32),
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

	// Write to memory buffer
	var buf bytes.Buffer
	if _, err := writer.WriteTo(&buf); err != nil {
		log.Fatalf("failed to write mmdb to buffer: %v", err)
	}

	return buf.Bytes()
}
