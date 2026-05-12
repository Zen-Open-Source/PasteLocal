//go:build integration

package auth

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestIntegration_KeychainRoundTrip tests the full keychain lifecycle
// (store + retrieve + delete) using the native macOS Keychain. It only
// runs on macOS where the keychain is available.
func TestIntegration_KeychainRoundTrip(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("keychain integration test only runs on macOS")
	}

	if !keychainAvailable() {
		t.Skip("/usr/bin/security not found; keychain unavailable")
	}

	token, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	// Clean up any pre-existing entry so the test starts from a known state.
	deleteKeychain()

	// Store the token in the keychain.
	if err := storeKeychain(token); err != nil {
		t.Fatalf("storeKeychain: %v", err)
	}

	// Retrieve the token and verify it matches.
	retrieved, err := retrieveKeychain()
	if err != nil {
		t.Fatalf("retrieveKeychain: %v", err)
	}
	if retrieved != token {
		t.Errorf("retrieved token = %q, want %q", retrieved, token)
	}

	// Delete the token from the keychain.
	if err := deleteKeychain(); err != nil {
		t.Fatalf("deleteKeychain: %v", err)
	}

	// Verify the token is gone.
	_, err = retrieveKeychain()
	if err == nil {
		t.Error("retrieveKeychain should fail after deletion, but succeeded")
	}
}

// TestIntegration_TokenFileRoundTrip tests the token file lifecycle
// (store + retrieve + delete) against a real filesystem.
func TestIntegration_TokenFileRoundTrip(t *testing.T) {
	dir, err := os.MkdirTemp("", "clipbridge-auth-integration-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	tokenPath := filepath.Join(dir, "token")
	token, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	// Store the token to a file.
	if err := StoreTokenFile(token, tokenPath); err != nil {
		t.Fatalf("StoreTokenFile: %v", err)
	}

	// Verify the file has the correct permissions (0600).
	info, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatalf("stat token file: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("file permissions = %04o, want 0600", info.Mode().Perm())
	}

	// Retrieve the token and verify it matches.
	retrieved, err := RetrieveTokenFile(tokenPath)
	if err != nil {
		t.Fatalf("RetrieveTokenFile: %v", err)
	}
	if retrieved != token {
		t.Errorf("retrieved token = %q, want %q", retrieved, token)
	}

	// Delete the token file.
	if err := DeleteTokenFile(tokenPath); err != nil {
		t.Fatalf("DeleteTokenFile: %v", err)
	}

	// Verify the file is gone.
	if _, err := os.Stat(tokenPath); !os.IsNotExist(err) {
		t.Error("token file should be deleted after DeleteTokenFile")
	}
}
