//go:build !darwin && !linux

package auth

import "errors"

// keychainAvailable returns false on unsupported platforms.
func keychainAvailable() bool {
	return false
}

// storeKeychain always returns an error on unsupported platforms.
func storeKeychain(token string) error {
	return errors.New("keychain not supported on this platform")
}

// retrieveKeychain always returns an error on unsupported platforms.
func retrieveKeychain() (string, error) {
	return "", errors.New("keychain not supported on this platform")
}

// deleteKeychain always returns an error on unsupported platforms.
func deleteKeychain() error {
	return errors.New("keychain not supported on this platform")
}
