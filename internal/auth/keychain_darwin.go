//go:build darwin

package auth

import (
	"fmt"
	"os/exec"
)

const (
	keychainService = "com.clipbridge.daemon"
	keychainAccount = "auth-token"
	securityBin     = "/usr/bin/security"
)

// StoreTokenKeychain stores the token in the macOS Keychain using
// /usr/bin/security add-generic-password. No CGO is used.
func StoreTokenKeychain(token string) error {
	cmd := exec.Command(securityBin,
		"add-generic-password",
		"-a", keychainAccount,
		"-s", keychainService,
		"-w", token,
		"-U", // update if exists
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("storing token in keychain: %w: %s", err, out)
	}
	return nil
}

// RetrieveTokenKeychain retrieves the token from the macOS Keychain using
// /usr/bin/security find-generic-password.
func RetrieveTokenKeychain() (string, error) {
	cmd := exec.Command(securityBin,
		"find-generic-password",
		"-a", keychainAccount,
		"-s", keychainService,
		"-w", // write password to stdout
	)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("retrieving token from keychain: %w", err)
	}
	// Trim trailing newline that security outputs
	token := string(out)
	if len(token) > 0 && token[len(token)-1] == '\n' {
		token = token[:len(token)-1]
	}
	return token, nil
}

// DeleteTokenKeychain deletes the token from the macOS Keychain using
// /usr/bin/security delete-generic-password. Returns nil if the item
// does not exist in the keychain.
func DeleteTokenKeychain() error {
	cmd := exec.Command(securityBin,
		"delete-generic-password",
		"-a", keychainAccount,
		"-s", keychainService,
	)
	_, err := cmd.CombinedOutput()
	if err != nil {
		// Exit code 44 means "The specified item could not be found in the keychain."
		// Treat this as success, similar to how DeleteTokenFile treats missing files.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 44 {
			return nil
		}
		return fmt.Errorf("deleting token from keychain: %w", err)
	}
	return nil
}

// keychainAvailable returns true if /usr/bin/security is present on the system.
func keychainAvailable() bool {
	p, err := exec.LookPath(securityBin)
	return err == nil && p != ""
}

// storeKeychain stores the token in the platform-specific keychain.
func storeKeychain(token string) error {
	return StoreTokenKeychain(token)
}

// retrieveKeychain retrieves the token from the platform-specific keychain.
func retrieveKeychain() (string, error) {
	return RetrieveTokenKeychain()
}

// deleteKeychain deletes the token from the platform-specific keychain.
func deleteKeychain() error {
	return DeleteTokenKeychain()
}
