//go:build linux

package auth

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrSecretToolNotAvailable is returned when the secret-tool binary
// cannot be found on the system.
var ErrSecretToolNotAvailable = errors.New("secret-tool not available")

const secretToolBin = "secret-tool"

// StoreTokenSecretTool stores the token using the Linux secret-tool
// (libsecret) command-line utility.
func StoreTokenSecretTool(token string) error {
	if !secretToolAvailable() {
		return ErrSecretToolNotAvailable
	}
	cmd := exec.Command(secretToolBin,
		"store",
		"--label=clipbridge",
		"service", "clipbridge",
		"user", "token",
	)
	cmd.Stdin = strings.NewReader(token + "\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("storing token with secret-tool: %w: %s", err, out)
	}
	return nil
}

// RetrieveTokenSecretTool retrieves the token using the Linux secret-tool
// (libsecret) command-line utility.
func RetrieveTokenSecretTool() (string, error) {
	if !secretToolAvailable() {
		return "", ErrSecretToolNotAvailable
	}
	cmd := exec.Command(secretToolBin,
		"lookup",
		"service", "clipbridge",
		"user", "token",
	)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("retrieving token with secret-tool: %w", err)
	}
	token := string(out)
	if len(token) > 0 && token[len(token)-1] == '\n' {
		token = token[:len(token)-1]
	}
	return token, nil
}

// DeleteTokenSecretTool deletes the token using the Linux secret-tool
// (libsecret) command-line utility.
func DeleteTokenSecretTool() error {
	if !secretToolAvailable() {
		return ErrSecretToolNotAvailable
	}
	cmd := exec.Command(secretToolBin,
		"clear",
		"service", "clipbridge",
		"user", "token",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("deleting token with secret-tool: %w: %s", err, out)
	}
	return nil
}

// secretToolAvailable returns true if secret-tool is found on PATH.
func secretToolAvailable() bool {
	_, err := exec.LookPath(secretToolBin)
	return err == nil
}

// keychainAvailable returns true if secret-tool is available on the system.
func keychainAvailable() bool {
	return secretToolAvailable()
}

// storeKeychain stores the token in the platform-specific secret store.
func storeKeychain(token string) error {
	return StoreTokenSecretTool(token)
}

// retrieveKeychain retrieves the token from the platform-specific secret store.
func retrieveKeychain() (string, error) {
	return RetrieveTokenSecretTool()
}

// deleteKeychain deletes the token from the platform-specific secret store.
func deleteKeychain() error {
	return DeleteTokenSecretTool()
}
