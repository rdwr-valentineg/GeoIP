package utils

import (
	"archive/tar"
	"bytes"
	"io"
	"testing"
)

func TestExtractFileFromTar(t *testing.T) {
	// Create a test tar archive in memory
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Add test files
	files := []struct {
		name    string
		content string
	}{
		{"test1.txt", "content1"},
		{"GeoLite2-Country.mmdb", "database content"},
		{"test2.txt", "content2"},
	}

	for _, file := range files {
		hdr := &tar.Header{
			Name:     file.name,
			Mode:     0644,
			Size:     int64(len(file.content)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(file.content)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()

	// Test extracting the database file
	tr := tar.NewReader(&buf)
	reader, size, err := ExtractFileFromTar(tr, "GeoLite2-Country.mmdb")
	if err != nil {
		t.Fatalf("Failed to extract file: %v", err)
	}

	if size != int64(len("database content")) {
		t.Errorf("Expected size %d, got %d", len("database content"), size)
	}

	// Read the content
	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("Failed to read extracted content: %v", err)
	}

	if string(content) != "database content" {
		t.Errorf("Expected content 'database content', got '%s'", string(content))
	}
}

func TestExtractFileFromTar_FileNotFound(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	tw.Close()

	tr := tar.NewReader(&buf)
	_, _, err := ExtractFileFromTar(tr, "nonexistent.mmdb")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

func TestExtractFileFromTar_DirectoryTraversal(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Try to add a malicious file with directory traversal
	hdr := &tar.Header{
		Name:     "../../../etc/passwd",
		Mode:     0644,
		Size:     5,
		Typeflag: tar.TypeReg,
	}
	tw.WriteHeader(hdr)
	tw.Write([]byte("pwned"))
	tw.Close()

	tr := tar.NewReader(&buf)
	_, _, err := ExtractFileFromTar(tr, "passwd")
	if err == nil {
		t.Error("Expected error for directory traversal attempt")
	}
}
