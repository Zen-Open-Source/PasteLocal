package auth

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// DefaultTokenPath returns the default file path for token storage:
// ~/.config/clipbridge/token
func DefaultTokenPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "clipbridge", "token")
}

// StoreTokenFile writes the token to the file at the given path with
// permissions restricted to owner-only (mode 0600).
func StoreTokenFile(token, path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating token directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(token), 0600); err != nil {
		return fmt.Errorf("writing token file: %w", err)
	}
	return nil
}

// RetrieveTokenFile reads the token from the file at the given path.
func RetrieveTokenFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading token file: %w", err)
	}
	return string(data), nil
}

// DeleteTokenFile removes the token file at the given path.
func DeleteTokenFile(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("deleting token file: %w", err)
	}
	return nil
}

// CheckTokenFilePerms checks that the token file at the given path has
// exactly mode 0600. Returns an error if the file has different permissions.
func CheckTokenFilePerms(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("checking token file: %w", err)
	}
	perm := info.Mode().Perm()
	if perm != fs.FileMode(0600) {
		return fmt.Errorf("token file has insecure permissions %04o, expected 0600", perm)
	}
	return nil
}
