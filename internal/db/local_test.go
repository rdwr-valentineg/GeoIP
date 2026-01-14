package db

import (
	"os"
	"testing"
)

func TestDiskLoader_LoadsAndReloads(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "geoip-db-*.mmdb")
	if err != nil {
		t.Fatalf("should have passed, failed to create temp file: %v", err)
	}

	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(GenerateValidMockMMDB()); err != nil {
		t.Fatalf("shoulg have passed, failed to write to temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("shoulg have passed, failed to close temp file: %v", err)
	}

	loader := NewDiskLoader(tmpFile.Name())
	if err = loader.Start(); err != nil {
		t.Fatalf("failed to start loader: %v", err)
	}
	if ready := loader.IsReady(); !ready {
		t.Fatalf("loader should be ready after start, got: %v", ready)
	}
	if reader := loader.GetReader(); reader == nil {
		t.Fatalf("loader should have a reader after start, got: %v", reader)
	}

	if err := loader.Reload(); err != nil {
		t.Fatalf("should have passed, failed to reload: %v", err)
	}
	if err := loader.Stop(); err != nil {
		t.Fatalf("should have passed, failed to stop loader: %v", err)
	}
}

func TestStopWithNoReader(t *testing.T) {
	loader := NewDiskLoader("nonexistent.mmdb")
	if err := loader.Stop(); err != nil {
		t.Fatalf("should have passed, failed to stop loader with no reader: %v", err)
	}
	if ready := loader.IsReady(); ready {
		t.Fatalf("loader should not be ready after stop with no reader, got: %v", ready)
	}
}

func TestReloadWithInvalidPath(t *testing.T) {
	loader := NewDiskLoader("invalid-path.mmdb")
	if err := loader.Reload(); err == nil {
		t.Fatalf("should have failed, expected error when reloading with invalid path, got nil")
	}
	if ready := loader.IsReady(); ready {
		t.Fatalf("loader should not be ready after reload with invalid path, got: %v", ready)
	}
}
