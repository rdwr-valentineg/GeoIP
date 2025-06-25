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

	if _, err := tmpFile.Write(GenerateMockMMDB()); err != nil {
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
}
