package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicReplaceFile(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "test_atomic_replace")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	targetPath := filepath.Join(tempDir, "target.txt")
	tmpPath := filepath.Join(tempDir, "temp.txt")

	// Create original file
	if err := os.WriteFile(targetPath, []byte("original content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create temporary file
	if err := os.WriteFile(tmpPath, []byte("new content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Test atomic replacement
	if err := AtomicReplaceFile(tmpPath, targetPath); err != nil {
		t.Fatalf("AtomicReplaceFile failed: %v", err)
	}

	// Verify the content was replaced
	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}

	if string(content) != "new content" {
		t.Errorf("Expected 'new content', got '%s'", string(content))
	}

	// Verify temporary file was removed
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("Temporary file should have been removed")
	}

	// Verify backup file was removed
	backupPath := targetPath + ".backup"
	if _, err := os.Stat(backupPath); !os.IsNotExist(err) {
		t.Error("Backup file should have been removed")
	}
}

func TestAtomicReplaceFile_NewFile(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "test_atomic_replace_new")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	targetPath := filepath.Join(tempDir, "target.txt")
	tmpPath := filepath.Join(tempDir, "temp.txt")

	// Create temporary file (no existing target)
	if err := os.WriteFile(tmpPath, []byte("new content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Test atomic replacement
	if err := AtomicReplaceFile(tmpPath, targetPath); err != nil {
		t.Fatalf("AtomicReplaceFile failed: %v", err)
	}

	// Verify the content was created
	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}

	if string(content) != "new content" {
		t.Errorf("Expected 'new content', got '%s'", string(content))
	}
}

func TestCreateTempFile(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "test_create_temp")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	basePath := filepath.Join(tempDir, "test.txt")

	// Test creating temporary file
	file, tmpPath, err := CreateTempFile(basePath)
	if err != nil {
		t.Fatalf("CreateTempFile failed: %v", err)
	}
	defer file.Close()

	// Verify the path
	expectedPath := basePath + ".tmp"
	if tmpPath != expectedPath {
		t.Errorf("Expected path '%s', got '%s'", expectedPath, tmpPath)
	}

	// Verify the file exists
	if _, err := os.Stat(tmpPath); err != nil {
		t.Errorf("Temporary file should exist: %v", err)
	}

	// Test writing to the file
	if _, err := file.WriteString("test content"); err != nil {
		t.Fatalf("Failed to write to temporary file: %v", err)
	}

	file.Close()

	// Verify content
	content, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatal(err)
	}

	if string(content) != "test content" {
		t.Errorf("Expected 'test content', got '%s'", string(content))
	}
}
