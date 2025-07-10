package utils

import (
	"os"

	"github.com/pkg/errors"
)

// AtomicReplaceFile atomically replaces a target file with a temporary file.
// It creates a backup of the existing file, replaces it with the new file,
// and cleans up the backup on success. If the replacement fails, it restores
// the backup.
func AtomicReplaceFile(tmpPath, targetPath string) error {
	backupPath := targetPath + ".backup"

	// Create backup of existing file if it exists
	if _, err := os.Stat(targetPath); err == nil {
		if err := os.Rename(targetPath, backupPath); err != nil {
			return errors.Wrap(err, "failed to backup existing file")
		}
	}

	// Replace with new file
	if err := os.Rename(tmpPath, targetPath); err != nil {
		// Restore backup if rename fails
		if restoreErr := os.Rename(backupPath, targetPath); restoreErr != nil {
			// Log this error but return the original error
			// In a real application, you might want to use a logger here
		}
		return errors.Wrap(err, "failed to rename temporary file")
	}

	// Clean up backup on success
	os.Remove(backupPath)
	return nil
}

// CreateTempFile creates a temporary file with the given base path.
// The temporary file will have a ".tmp" suffix.
func CreateTempFile(basePath string) (*os.File, string, error) {
	tmpPath := basePath + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return nil, "", errors.Wrap(err, "failed to create temporary file")
	}
	return file, tmpPath, nil
}
