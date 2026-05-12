package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
)

// GenerateToken generates a URL-safe base64 encoded string from 32 random bytes
// sourced from crypto/rand. The resulting token must never appear in logs.
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating random bytes: %w", err)
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// ValidateToken compares a provided token against the expected token using
// constant-time comparison to prevent timing side-channel attacks.
func ValidateToken(provided, expected string) bool {
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}
