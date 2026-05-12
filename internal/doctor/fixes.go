package doctor

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/clipbridge/clipbridge/internal/auth"
	"github.com/clipbridge/clipbridge/internal/config"
	"github.com/clipbridge/clipbridge/internal/hostinstall"
	"github.com/clipbridge/clipbridge/internal/sshconfig"
)

// defaultTokenPathOverride allows tests to override the token path.
// When non-empty, it is used instead of auth.DefaultTokenPath().
var defaultTokenPathOverride string

// resolvedTokenPath returns the token path, respecting the test override.
func resolvedTokenPath() string {
	if defaultTokenPathOverride != "" {
		return defaultTokenPathOverride
	}
	return auth.DefaultTokenPath()
}

// fixMissingKeychain re-stores the token in the system keychain.
func fixMissingKeychain(cfg *config.Config) error {
	tokenPath := resolvedTokenPath()
	if tokenPath == "" {
		return fmt.Errorf("cannot determine token path")
	}

	// Read the token from the file.
	token, err := auth.RetrieveTokenFile(tokenPath)
	if err != nil {
		return fmt.Errorf("read token file: %w", err)
	}

	// Store in keychain via the platform-specific implementation.
	store := auth.NewTokenStore(true, tokenPath)
	if err := store.Store(token); err != nil {
		return fmt.Errorf("store token in keychain: %w", err)
	}
	return nil
}

// fixMissingRemoteBinary re-copies the clipbridge-remote binary to the host via SCP.
func fixMissingRemoteBinary(cfg *config.Config, alias string) error {
	hostCfg, ok := cfg.Hosts[alias]
	if !ok {
		return fmt.Errorf("host %q not in config", alias)
	}

	sshHost := sshHostStr(hostCfg, alias)
	remotePath := remoteBinPath(hostCfg)

	// Detect the remote architecture.
	arch, err := hostinstall.DetectRemoteArch(sshHost)
	if err != nil {
		return fmt.Errorf("detect remote architecture: %w", err)
	}

	// Find the local binary for this architecture.
	binaryName := fmt.Sprintf("clipbridge-remote-%s", arch)
	localBinaryDir := filepath.Join(userHomeDir(), ".local", "share", "clipbridge")
	localBinary := filepath.Join(localBinaryDir, binaryName)

	// Check if the binary exists at the standard embedded location.
	if _, err := os.Stat(localBinary); err != nil {
		// Try the current executable's directory as a fallback.
		exePath, exeErr := os.Executable()
		if exeErr == nil {
			altDir := filepath.Dir(exePath)
			altBinary := filepath.Join(altDir, binaryName)
			if _, statErr := os.Stat(altBinary); statErr == nil {
				localBinary = altBinary
			} else {
				return fmt.Errorf("local binary not found: %s: %w", localBinary, err)
			}
		} else {
			return fmt.Errorf("local binary not found: %s: %w", localBinary, err)
		}
	}

	// Ensure remote directory exists.
	remoteDir := filepath.Dir(remotePath)
	if _, _, err := hostinstall.RunSSH(sshHost, "mkdir", "-p", remoteDir); err != nil {
		return fmt.Errorf("create remote directory %s: %w", remoteDir, err)
	}

	// SCP the binary.
	if err := hostinstall.SCP(localBinary, sshHost, remotePath); err != nil {
		return fmt.Errorf("scp binary: %w", err)
	}

	// Set executable permissions.
	if err := hostinstall.SetRemotePerms(sshHost, remotePath, "0755"); err != nil {
		return fmt.Errorf("set binary permissions: %w", err)
	}

	return nil
}

// fixMissingSkill re-copies the skill file (paste.md) to the remote host.
func fixMissingSkill(cfg *config.Config, alias string) error {
	hostCfg, ok := cfg.Hosts[alias]
	if !ok {
		return fmt.Errorf("host %q not in config", alias)
	}

	sshHost := sshHostStr(hostCfg, alias)

	// Find the local skill file.
	skillPath := findLocalSkillPath()
	if skillPath == "" {
		return fmt.Errorf("local skill file (paste.md) not found")
	}

	// Ensure the remote directory exists.
	const skillDir = "~/.claude/commands"
	if _, _, err := hostinstall.RunSSH(sshHost, "mkdir", "-p", skillDir); err != nil {
		return fmt.Errorf("create remote skill directory: %w", err)
	}

	// SCP the skill file.
	const remoteSkillPath = "~/.claude/commands/paste.md"
	if err := hostinstall.SCP(skillPath, sshHost, remoteSkillPath); err != nil {
		return fmt.Errorf("scp skill file: %w", err)
	}

	return nil
}

// fixMissingRemoteForward adds a RemoteForward line to the SSH config for the host.
func fixMissingRemoteForward(cfg *config.Config, alias string) error {
	hostCfg, ok := cfg.Hosts[alias]
	if !ok {
		return fmt.Errorf("host %q not in config", alias)
	}

	port := hostCfg.RemotePort
	if port == 0 {
		port = cfg.Port
	}

	sshCfgPath := userSSHConfigPath()

	// Read existing config or start fresh.
	content, err := os.ReadFile(sshCfgPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read ssh config: %w", err)
		}
		content = []byte{}
	}

	parsed, err := sshconfig.Parse(string(content))
	if err != nil {
		return fmt.Errorf("parse ssh config: %w", err)
	}

	if err := sshconfig.AddRemoteForward(parsed, alias, port); err != nil {
		return fmt.Errorf("add remote forward: %w", err)
	}

	if err := sshconfig.WriteAtomic(sshCfgPath, parsed.Render()); err != nil {
		return fmt.Errorf("write ssh config: %w", err)
	}

	return nil
}

// fixTokenPerms corrects the local token file permissions to 0600.
func fixTokenPerms(cfg *config.Config) error {
	tokenPath := resolvedTokenPath()
	if tokenPath == "" {
		return fmt.Errorf("cannot determine token path")
	}

	if err := os.Chmod(tokenPath, 0600); err != nil {
		return fmt.Errorf("chmod %s: %w", tokenPath, err)
	}
	return nil
}

// fixRemoteTokenPerms corrects the remote token file permissions to 0600.
func fixRemoteTokenPerms(cfg *config.Config, alias string) error {
	hostCfg, ok := cfg.Hosts[alias]
	if !ok {
		return fmt.Errorf("host %q not in config", alias)
	}

	sshHost := sshHostStr(hostCfg, alias)
	const remoteTokenPath = "~/.config/clipbridge/token"

	if err := hostinstall.SetRemotePerms(sshHost, remoteTokenPath, "0600"); err != nil {
		return fmt.Errorf("set remote token permissions: %w", err)
	}
	return nil
}

// findLocalSkillPath searches for the local paste.md skill file.
func findLocalSkillPath() string {
	// Check the project skill directory.
	home := userHomeDir()

	// Try standard locations.
	candidates := []string{
		filepath.Join(home, ".config", "clipbridge", "skill", "paste.md"),
		filepath.Join(home, ".local", "share", "clipbridge", "skill", "paste.md"),
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// Try relative to the executable.
	exePath, err := os.Executable()
	if err == nil {
		candidates = append(candidates,
			filepath.Join(filepath.Dir(exePath), "..", "skill", "paste.md"),
		)
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	return ""
}
