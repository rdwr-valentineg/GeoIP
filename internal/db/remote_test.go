package db

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
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
	arch, err := CreateTarGz(mockDB, "GeoLite2-Country.mmdb")
	if err != nil {
		t.Error(err)
	}
	// Setup mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(arch)
	}))
	defer server.Close()

	// Patch URL for test
	oldURL := maxmindBaseURL
	maxmindBaseURL = server.URL
	defer func() { maxmindBaseURL = oldURL }()

	remote := &RemoteFetcher{
		BasicAuth: "dummy",
		DBPath:    "",
		Interval:  time.Hour,
		Client:    server.Client(),
		inMemory:  true,
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

func TestExtractFileFromTar_FindsFile(t *testing.T) {
	content := []byte("hello world")
	filename := "GeoLite2-Country.mmdb"
	archive, err := CreateTarGz(content, filename)
	if err != nil {
		t.Fatalf("failed to create tar.gz: %v", err)
	}

	// Un-gzip
	gzr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)

	r, size, err := extractFileFromTar(tr, filename)
	if err != nil {
		t.Fatalf("extractFileFromTar failed: %v", err)
	}
	if size != int64(len(content)) {
		t.Errorf("expected size %d, got %d", len(content), size)
	}
	got := make([]byte, size)
	if _, err := io.ReadFull(r, got); err != nil {
		t.Fatalf("failed to read file from tar: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("expected content %q, got %q", content, got)
	}
}

func TestExtractFileFromTar_FileNotFound(t *testing.T) {
	content := []byte("irrelevant")
	archive, err := CreateTarGz(content, "somefile.txt")
	if err != nil {
		t.Fatalf("failed to create tar.gz: %v", err)
	}

	gzr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)

	r, size, err := extractFileFromTar(tr, "GeoLite2-Country.mmdb")
	if err == nil {
		t.Fatalf("expected error for missing file, got nil")
	}
	if r != nil || size != 0 {
		t.Errorf("expected nil reader and size 0, got %v, %d", r, size)
	}
}

func TestExtractFileFromTar_SkipsNonRegularFiles(t *testing.T) {
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	// Add a directory entry
	dirHeader := &tar.Header{
		Name:     "adir/",
		Typeflag: tar.TypeDir,
		Mode:     0755,
		ModTime:  time.Now(),
	}
	if err := tw.WriteHeader(dirHeader); err != nil {
		t.Fatalf("failed to write dir header: %v", err)
	}

	// Add the target file
	content := []byte("abc")
	fileHeader := &tar.Header{
		Name:     "adir/GeoLite2-Country.mmdb",
		Typeflag: tar.TypeReg,
		Mode:     0644,
		Size:     int64(len(content)),
		ModTime:  time.Now(),
	}
	if err := tw.WriteHeader(fileHeader); err != nil {
		t.Fatalf("failed to write file header: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("failed to write file content: %v", err)
	}
	tw.Close()
	gzw.Close()

	gzr, err := gzip.NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)

	r, size, err := extractFileFromTar(tr, "GeoLite2-Country.mmdb")
	if err != nil {
		t.Fatalf("extractFileFromTar failed: %v", err)
	}
	got := make([]byte, size)
	if _, err := io.ReadFull(r, got); err != nil {
		t.Fatalf("failed to read file from tar: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("expected content %q, got %q", content, got)
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

	// Write to memory buffer
	var buf bytes.Buffer
	if _, err := writer.WriteTo(&buf); err != nil {
		log.Fatalf("failed to write mmdb to buffer: %v", err)
	}

	return buf.Bytes()
}

// CreateTarGz creates a .tar.gz archive in memory with one file.
func CreateTarGz(data []byte, filename string) ([]byte, error) {
	var buf bytes.Buffer

	// gzip writer
	gzw := gzip.NewWriter(&buf)
	defer gzw.Close()

	// tar writer inside gzip
	tw := tar.NewWriter(gzw)
	defer tw.Close()

	// Write tar header
	hdr := &tar.Header{
		Name:    filename,
		Mode:    0644,
		Size:    int64(len(data)),
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return nil, err
	}

	// Write file content
	if _, err := tw.Write(data); err != nil {
		return nil, err
	}

	// Close both writers to flush buffers
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gzw.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
