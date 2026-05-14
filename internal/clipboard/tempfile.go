package clipboard

import (
	"fmt"
	"os"
	"path/filepath"
)

// tempFile writes data to a temporary file with the given suffix and
// returns the path and a cleanup function.
func tempFile(data []byte, suffix string) (string, func(), error) {
	f, err := os.CreateTemp("", "pastelocal-*"+suffix)
	if err != nil {
		return "", nil, fmt.Errorf("creating temp file: %w", err)
	}
	path := f.Name()

	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(path)
		return "", nil, fmt.Errorf("writing temp file: %w", err)
	}

	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", nil, fmt.Errorf("closing temp file: %w", err)
	}

	cleanup := func() { os.Remove(path) }
	return path, cleanup, nil
}

// tempFileInDir writes data to a temporary file in the given directory
// with the given suffix and returns the path and a cleanup function.
func tempFileInDir(dir string, data []byte, suffix string) (string, func(), error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", nil, fmt.Errorf("creating directory: %w", err)
	}
	f, err := os.CreateTemp(dir, "pastelocal-*"+suffix)
	if err != nil {
		return "", nil, fmt.Errorf("creating temp file: %w", err)
	}
	path := f.Name()

	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(path)
		return "", nil, fmt.Errorf("writing temp file: %w", err)
	}

	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", nil, fmt.Errorf("closing temp file: %w", err)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		os.Remove(path)
		return "", nil, fmt.Errorf("resolving absolute path: %w", err)
	}

	cleanup := func() { os.Remove(absPath) }
	return absPath, cleanup, nil
}
