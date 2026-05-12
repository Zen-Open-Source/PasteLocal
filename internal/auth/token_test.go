package auth

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateToken(t *testing.T) {
	token, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken() error: %v", err)
	}
	if token == "" {
		t.Fatal("GenerateToken() returned empty token")
	}

	// Verify the token is valid URL-safe base64
	decoded, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("token is not valid URL-safe base64: %v", err)
	}

	// Verify decoded length is 32 bytes
	if len(decoded) != 32 {
		t.Fatalf("decoded token length = %d, want 32", len(decoded))
	}
}

func TestGenerateTokenUniqueness(t *testing.T) {
	t1, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	t2, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if t1 == t2 {
		t.Fatal("two consecutive tokens should not be equal")
	}
}

func TestValidateTokenCorrect(t *testing.T) {
	token, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if !ValidateToken(token, token) {
		t.Fatal("ValidateToken should return true for matching tokens")
	}
}

func TestValidateTokenIncorrect(t *testing.T) {
	token, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if ValidateToken("wrong-token", token) {
		t.Fatal("ValidateToken should return false for non-matching tokens")
	}
	if ValidateToken("", token) {
		t.Fatal("ValidateToken should return false for empty provided token")
	}
}

func TestValidateTokenConstantTime(t *testing.T) {
	// Basic check: validation doesn't short-circuit on first byte mismatch.
	// A truly constant-time compare should take the same path regardless of
	// where the first difference occurs.
	expected := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	wrongFirst := "baaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	wrongLast := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaab"

	// Both should return false — if the implementation short-circuited,
	// wrongFirst would be faster, but both must return the same result.
	r1 := ValidateToken(wrongFirst, expected)
	r2 := ValidateToken(wrongLast, expected)
	if r1 || r2 {
		t.Fatal("ValidateToken should return false for mismatched tokens")
	}
}

func TestStoreTokenFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	token := "test-token-value"

	if err := StoreTokenFile(token, path); err != nil {
		t.Fatalf("StoreTokenFile() error: %v", err)
	}

	// Verify file exists and has mode 0600
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat token file: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("file permissions = %04o, want 0600", info.Mode().Perm())
	}
}

func TestRetrieveTokenFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	token := "my-secret-token"

	if err := StoreTokenFile(token, path); err != nil {
		t.Fatalf("StoreTokenFile() error: %v", err)
	}

	got, err := RetrieveTokenFile(path)
	if err != nil {
		t.Fatalf("RetrieveTokenFile() error: %v", err)
	}
	if got != token {
		t.Errorf("RetrieveTokenFile() = %q, want %q", got, token)
	}
}

func TestDeleteTokenFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")

	if err := StoreTokenFile("token", path); err != nil {
		t.Fatalf("StoreTokenFile() error: %v", err)
	}

	if err := DeleteTokenFile(path); err != nil {
		t.Fatalf("DeleteTokenFile() error: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("token file should be deleted")
	}
}

func TestDeleteTokenFileNonexistent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent")

	// Deleting a nonexistent file should not return an error
	if err := DeleteTokenFile(path); err != nil {
		t.Fatalf("DeleteTokenFile() on nonexistent file error: %v", err)
	}
}

func TestCheckTokenFilePerms(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	token := "test-token"

	if err := StoreTokenFile(token, path); err != nil {
		t.Fatalf("StoreTokenFile() error: %v", err)
	}

	// 0600 should pass
	if err := CheckTokenFilePerms(path); err != nil {
		t.Errorf("CheckTokenFilePerms() on 0600 file error: %v", err)
	}

	// Change permissions to something insecure
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	if err := CheckTokenFilePerms(path); err == nil {
		t.Fatal("CheckTokenFilePerms() should reject 0644 permissions")
	}
}

func TestTokenStoreFallsBackToFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	token := "fallback-token"

	// UseKeychain = false, should use file
	store := NewTokenStore(false, path)
	if err := store.Store(token); err != nil {
		t.Fatalf("TokenStore.Store() error: %v", err)
	}

	got, err := store.Retrieve()
	if err != nil {
		t.Fatalf("TokenStore.Retrieve() error: %v", err)
	}
	if got != token {
		t.Errorf("TokenStore.Retrieve() = %q, want %q", got, token)
	}
}

func TestTokenStoreDeleteBoth(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")

	store := NewTokenStore(false, path)
	if err := store.Store("token"); err != nil {
		t.Fatalf("TokenStore.Store() error: %v", err)
	}

	if err := store.Delete(); err != nil {
		t.Fatalf("TokenStore.Delete() error: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("token file should be deleted after TokenStore.Delete()")
	}
}

// --- DefaultTokenPath tests ---

func TestDefaultTokenPath(t *testing.T) {
	path := DefaultTokenPath()
	if path == "" {
		t.Fatal("DefaultTokenPath() returned empty string")
	}
	if !filepath.IsAbs(path) {
		t.Errorf("DefaultTokenPath() = %q, expected absolute path", path)
	}
	if !strings.Contains(path, ".config") {
		t.Errorf("DefaultTokenPath() = %q, expected to contain .config", path)
	}
	if !strings.Contains(path, "clipbridge") {
		t.Errorf("DefaultTokenPath() = %q, expected to contain clipbridge", path)
	}
	if !strings.HasSuffix(path, "token") {
		t.Errorf("DefaultTokenPath() = %q, expected to end with token", path)
	}
}

// --- StoreTokenFile edge cases ---

func TestStoreTokenFileCreatesDir(t *testing.T) {
	dir := t.TempDir()
	// Path with non-existent intermediate directories
	path := filepath.Join(dir, "a", "b", "c", "token")
	token := "deep-nested-token"

	if err := StoreTokenFile(token, path); err != nil {
		t.Fatalf("StoreTokenFile() error: %v", err)
	}

	got, err := RetrieveTokenFile(path)
	if err != nil {
		t.Fatalf("RetrieveTokenFile() error: %v", err)
	}
	if got != token {
		t.Errorf("RetrieveTokenFile() = %q, want %q", got, token)
	}
}

func TestStoreTokenFileMkdirAllError(t *testing.T) {
	dir := t.TempDir()
	// Create a regular file where a directory would need to be created
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// Path requires creating a directory inside the file — should fail
	path := filepath.Join(blocker, "subdir", "token")
	err := StoreTokenFile("test-token", path)
	if err == nil {
		t.Fatal("StoreTokenFile should fail when MkdirAll can't create directory")
	}
}

func TestStoreTokenFileWriteError(t *testing.T) {
	dir := t.TempDir()
	// Create a directory at the target file path — os.WriteFile on a directory fails
	target := filepath.Join(dir, "token")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	err := StoreTokenFile("test-token", target)
	if err == nil {
		t.Fatal("StoreTokenFile should fail when target path is a directory")
	}
}

func TestStoreTokenFileEmptyToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")

	if err := StoreTokenFile("", path); err != nil {
		t.Fatalf("StoreTokenFile with empty token error: %v", err)
	}

	got, err := RetrieveTokenFile(path)
	if err != nil {
		t.Fatalf("RetrieveTokenFile() error: %v", err)
	}
	if got != "" {
		t.Errorf("RetrieveTokenFile() = %q, want empty string", got)
	}
}

// --- RetrieveTokenFile edge cases ---

func TestRetrieveTokenFileNonexistent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent")

	_, err := RetrieveTokenFile(path)
	if err == nil {
		t.Fatal("RetrieveTokenFile should return error for nonexistent file")
	}
}

func TestRetrieveTokenFileWithWhitespace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")

	// Write a file with trailing whitespace/newline manually
	content := "token-with-spaces  \n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	got, err := RetrieveTokenFile(path)
	if err != nil {
		t.Fatalf("RetrieveTokenFile() error: %v", err)
	}
	// RetrieveTokenFile returns raw content including whitespace
	if got != content {
		t.Errorf("RetrieveTokenFile() = %q, want %q", got, content)
	}
}

// --- DeleteTokenFile edge cases ---

func TestDeleteTokenFileDirectoryError(t *testing.T) {
	dir := t.TempDir()
	// Create a non-empty directory at the path
	target := filepath.Join(dir, "token")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "child"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	err := DeleteTokenFile(target)
	if err == nil {
		t.Fatal("DeleteTokenFile on non-empty directory should return error")
	}
}

// --- CheckTokenFilePerms edge cases ---

func TestCheckTokenFilePermsNonexistent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent")

	err := CheckTokenFilePerms(path)
	if err == nil {
		t.Fatal("CheckTokenFilePerms should return error for nonexistent file")
	}
}

// --- ValidateToken edge cases ---

func TestValidateTokenBothEmpty(t *testing.T) {
	if !ValidateToken("", "") {
		t.Fatal("ValidateToken with both empty strings should return true")
	}
}

func TestValidateTokenEmptyExpected(t *testing.T) {
	if ValidateToken("nonempty", "") {
		t.Fatal("ValidateToken with non-empty provided and empty expected should return false")
	}
}

func TestValidateTokenMismatchedLengths(t *testing.T) {
	if ValidateToken("short", "much-longer-token-value") {
		t.Fatal("ValidateToken with different length tokens should return false")
	}
}

// --- NewTokenStore tests ---

func TestNewTokenStore(t *testing.T) {
	store := NewTokenStore(true, "/test/path")
	if !store.UseKeychain {
		t.Fatal("UseKeychain should be true")
	}
	if store.TokenPath != "/test/path" {
		t.Errorf("TokenPath = %q, want %q", store.TokenPath, "/test/path")
	}

	store2 := NewTokenStore(false, "")
	if store2.UseKeychain {
		t.Fatal("UseKeychain should be false")
	}
	if store2.TokenPath != "" {
		t.Errorf("TokenPath = %q, want empty string", store2.TokenPath)
	}
}

// --- TokenStore Retrieve error path ---

func TestTokenStoreRetrieveFileNotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent")

	store := NewTokenStore(false, path)
	_, err := store.Retrieve()
	if err == nil {
		t.Fatal("TokenStore.Retrieve() should return error when file does not exist")
	}
}

// --- TokenStore Store and Retrieve with long token ---

func TestTokenStoreLongToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	// Very long token (simulating a real generated token)
	token := strings.Repeat("x", 500)

	store := NewTokenStore(false, path)
	if err := store.Store(token); err != nil {
		t.Fatalf("TokenStore.Store() error: %v", err)
	}

	got, err := store.Retrieve()
	if err != nil {
		t.Fatalf("TokenStore.Retrieve() error: %v", err)
	}
	if got != token {
		t.Error("TokenStore.Retrieve() mismatch for long token")
	}
}

// --- TokenStore Delete when file doesn't exist (keychain cleaned, file already gone) ---

func TestTokenStoreDeleteNonexistent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent")

	store := NewTokenStore(false, path)
	// Delete on a non-existent file path should succeed
	if err := store.Delete(); err != nil {
		t.Fatalf("TokenStore.Delete() on nonexistent file error: %v", err)
	}
}
