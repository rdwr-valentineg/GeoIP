package utils

import (
	"archive/tar"
	"fmt"
	"io"
	"strings"
)

// ExtractFileFromTar extracts a specific file from a tar archive.
// It searches for the first file whose name contains the target string.
// Returns a reader for the file content, the file size, and any error.
func ExtractFileFromTar(tr *tar.Reader, target string) (io.Reader, int64, error) {
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

		// Validate path to prevent directory traversal
		if strings.Contains(header.Name, "..") {
			continue
		}

		if strings.Contains(header.Name, target) {
			// Wrap in a LimitedReader to avoid reading beyond the file size
			return io.LimitReader(tr, header.Size), header.Size, nil
		}
	}
	return nil, 0, fmt.Errorf("file %s not found in archive", target)
}
