package auth

import "fmt"

// TokenStore provides a unified interface for token storage, preferring
// the system keychain when available and falling back to file-based storage.
type TokenStore struct {
	UseKeychain bool
	TokenPath   string // file fallback path
}

// NewTokenStore creates a new TokenStore. If useKeychain is true, the store
// will attempt to use the system keychain before falling back to file storage.
// tokenPath specifies the file path for fallback storage.
func NewTokenStore(useKeychain bool, tokenPath string) *TokenStore {
	return &TokenStore{
		UseKeychain: useKeychain,
		TokenPath:   tokenPath,
	}
}

// Store persists the token. It tries the keychain first (if enabled and
// available), then falls back to file-based storage.
func (s *TokenStore) Store(token string) error {
	if s.UseKeychain && keychainAvailable() {
		if err := storeKeychain(token); err != nil {
			// Fall back to file storage on keychain error
			return StoreTokenFile(token, s.TokenPath)
		}
		return nil
	}
	return StoreTokenFile(token, s.TokenPath)
}

// Retrieve fetches the stored token. It tries the keychain first (if enabled
// and available), then falls back to file-based storage.
func (s *TokenStore) Retrieve() (string, error) {
	if s.UseKeychain && keychainAvailable() {
		token, err := retrieveKeychain()
		if err == nil {
			return token, nil
		}
		// Fall back to file storage
	}
	return RetrieveTokenFile(s.TokenPath)
}

// Delete removes the token from both keychain and file storage to ensure
// complete cleanup regardless of which store was actually used.
func (s *TokenStore) Delete() error {
	var keychainErr, fileErr error

	if keychainAvailable() {
		keychainErr = deleteKeychain()
	}

	fileErr = DeleteTokenFile(s.TokenPath)

	if keychainErr != nil && fileErr != nil {
		return fmt.Errorf("keychain: %w; file: %w", keychainErr, fileErr)
	}
	if keychainErr != nil {
		return keychainErr
	}
	return fileErr
}
