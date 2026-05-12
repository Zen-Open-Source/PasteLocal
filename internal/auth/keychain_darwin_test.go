//go:build darwin

package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKeychainAvailable(t *testing.T) {
	if !keychainAvailable() {
		t.Fatal("keychainAvailable() should return true on darwin")
	}
}

func TestStoreRetrieveDeleteKeychain(t *testing.T) {
	token := "test-token-for-coverage-abc123"

	// Store in keychain
	err := StoreTokenKeychain(token)
	if err != nil {
		t.Skipf("keychain store failed, skipping integration test: %v", err)
	}

	// Retrieve from keychain
	got, err := RetrieveTokenKeychain()
	if err != nil {
		t.Fatalf("RetrieveTokenKeychain() error: %v", err)
	}
	if got != token {
		t.Errorf("RetrieveTokenKeychain() = %q, want %q", got, token)
	}

	// Delete from keychain
	if err := DeleteTokenKeychain(); err != nil {
		t.Fatalf("DeleteTokenKeychain() error: %v", err)
	}

	// Verify deletion — retrieve should fail
	_, err = RetrieveTokenKeychain()
	if err == nil {
		t.Fatal("RetrieveTokenKeychain() should fail after deletion")
	}
}

func TestRetrieveTokenKeychainNotFound(t *testing.T) {
	// Ensure no token exists in the keychain
	DeleteTokenKeychain()

	_, err := RetrieveTokenKeychain()
	if err == nil {
		t.Fatal("RetrieveTokenKeychain() should return error when no token stored")
	}
}

func TestDeleteTokenKeychainNotFound(t *testing.T) {
	// Ensure no token exists
	DeleteTokenKeychain()

	// Deleting a nonexistent entry should return nil (exit code 44)
	if err := DeleteTokenKeychain(); err != nil {
		t.Fatalf("DeleteTokenKeychain() on nonexistent entry should return nil, got: %v", err)
	}
}

func TestTokenStoreStoreKeychainEnabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	token := "keychain-store-test"

	// Clean up any existing keychain entry
	DeleteTokenKeychain()

	store := NewTokenStore(true, path)
	if err := store.Store(token); err != nil {
		t.Fatalf("TokenStore.Store() error: %v", err)
	}

	// Verify token can be retrieved
	got, err := store.Retrieve()
	if err != nil {
		t.Fatalf("TokenStore.Retrieve() error: %v", err)
	}
	if got != token {
		t.Errorf("TokenStore.Retrieve() = %q, want %q", got, token)
	}

	// Clean up
	store.Delete()
}

func TestTokenStoreRetrieveKeychainFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	token := "fallback-retrieve-token"

	// Ensure no keychain entry exists so Retrieve falls back to file
	DeleteTokenKeychain()

	// Store token to file only
	if err := StoreTokenFile(token, path); err != nil {
		t.Fatalf("StoreTokenFile() error: %v", err)
	}

	// Retrieve with keychain enabled — should try keychain first (fail),
	// then fall back to file
	store := NewTokenStore(true, path)
	got, err := store.Retrieve()
	if err != nil {
		t.Fatalf("TokenStore.Retrieve() with keychain fallback error: %v", err)
	}
	if got != token {
		t.Errorf("TokenStore.Retrieve() = %q, want %q", got, token)
	}
}

func TestTokenStoreStoreKeychainFallback(t *testing.T) {
	// This test verifies the fallback path when keychain is enabled
	// but the keychain store fails. Since we can't force keychain failure
	// on darwin, we test the structural behavior: the token ends up
	// accessible via the file path regardless.
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	token := "fallback-store-token"

	store := NewTokenStore(true, path)
	if err := store.Store(token); err != nil {
		t.Fatalf("TokenStore.Store() error: %v", err)
	}

	// Verify the token is readable from the file path
	// (if keychain succeeded, file may or may not exist;
	//  if keychain failed and fell back, file must exist)
	fileToken, fileErr := RetrieveTokenFile(path)

	// Regardless of keychain success/failure, the token should be retrievable
	got, err := store.Retrieve()
	if err != nil {
		t.Fatalf("TokenStore.Retrieve() error: %v", err)
	}
	if got != token {
		t.Errorf("TokenStore.Retrieve() = %q, want %q", got, token)
	}

	// If keychain failed, file must exist and match
	if fileErr == nil && fileToken != token {
		t.Errorf("file token = %q, want %q", fileToken, token)
	}
}

func TestTokenStoreDeleteBothStores(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	token := "delete-both-test"

	// Store in keychain
	err := StoreTokenKeychain(token)
	if err != nil {
		t.Skipf("keychain store failed, skipping: %v", err)
	}

	// Also store to file
	if err := StoreTokenFile(token, path); err != nil {
		t.Fatalf("StoreTokenFile() error: %v", err)
	}

	// Delete from both
	store := NewTokenStore(true, path)
	if err := store.Delete(); err != nil {
		t.Fatalf("TokenStore.Delete() error: %v", err)
	}

	// Verify file deleted
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("token file should be deleted")
	}

	// Verify keychain deleted
	_, err = RetrieveTokenKeychain()
	if err == nil {
		t.Fatal("keychain token should be deleted")
	}
}

func TestTokenStoreDeleteFileError(t *testing.T) {
	dir := t.TempDir()
	// Create a non-empty directory at the token path to make DeleteTokenFile fail
	tokenPath := filepath.Join(dir, "token")
	if err := os.MkdirAll(tokenPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tokenPath, "child"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewTokenStore(false, tokenPath)
	err := store.Delete()
	if err == nil {
		t.Fatal("TokenStore.Delete should fail when token path is a non-empty directory")
	}
}

func TestStoreTokenKeychainDirectError(t *testing.T) {
	// Test StoreTokenKeychain directly — the result depends on keychain access.
	// We just verify it doesn't panic and returns a sensible result.
	err := StoreTokenKeychain("direct-test-token")
	if err != nil {
		// Keychain not accessible — that's fine, error path is covered
		t.Logf("StoreTokenKeychain error (expected in some envs): %v", err)
	} else {
		// Keychain accessible — clean up
		DeleteTokenKeychain()
	}
}

func TestRetrieveTokenKeychainDirect(t *testing.T) {
	// Ensure no token is stored
	DeleteTokenKeychain()

	// Retrieve should fail
	_, err := RetrieveTokenKeychain()
	if err == nil {
		t.Fatal("RetrieveTokenKeychain() should fail when no token is stored")
	}
}
