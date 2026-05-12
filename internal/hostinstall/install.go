// Package hostinstall handles installing the clipbridge remote helper,
// skill, and token on remote hosts via SSH/SCP.
package hostinstall

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/clipbridge/clipbridge/internal/sshconfig"
)

const (
	defaultRemoteBinPath = "~/.local/bin/clipbridge-remote"
	defaultPort          = 7331
)

// Options holds the configuration for a host installation.
type Options struct {
	Alias           string // host alias name
	Host            string // SSH hostname/alias
	User            string // remote user (optional, uses SSH default)
	Port            int    // remote port (default 7331)
	Termius         bool   // skip ssh config edit
	Finish          bool   // just scp + skill install (after Termius manual step)
	Reinstall       bool   // reinstall even if already installed
	UpdateTokenOnly bool   // only scp the new token
	SeparateToken   bool   // generate per-host token
	RemotePath      string // default: ~/.local/bin/clipbridge-remote
	LocalBinaryDir  string // directory containing local clipbridge-remote binary
	TokenPath       string // local path to the token file
	SkillPath       string // local path to the skill file (paste.md)
}

// Installer performs the installation of clipbridge components on a remote host.
type Installer struct {
	opts   Options
	logger *slog.Logger
}

// NewInstaller creates a new Installer with the given options and logger.
func NewInstaller(opts Options, logger *slog.Logger) *Installer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Installer{opts: opts, logger: logger}
}

// Install performs the full host installation.
// Steps:
//  1. Determine remote arch via ssh <host> uname -m
//  2. SCP the appropriate clipbridge-remote binary
//  3. SCP the token file
//  4. SCP the skill file (paste.md) to ~/.claude/commands/paste.md
//  5. Set permissions (0600 on token, 0755 on binary, 0700 on dir)
//  6. Verify token file permissions on remote (abort if wrong)
//  7. If not --termius: edit ~/.ssh/config to add RemoteForward
func (i *Installer) Install() error {
	host := i.sshHost()
	binPath := i.binPath()
	port := i.port()

	// Step 0: If --update-token-only, only scp the token.
	if i.opts.UpdateTokenOnly {
		return i.installTokenOnly(host)
	}

	// Step 1: Detect remote architecture.
	arch, err := DetectRemoteArch(host)
	if err != nil {
		return fmt.Errorf("detect remote architecture: %w", err)
	}
	i.logger.Info("detected remote architecture", "arch", arch, "host", host)

	// Step 2: SCP the appropriate clipbridge-remote binary.
	binaryName := fmt.Sprintf("clipbridge-remote-%s", arch)
	localBinary := filepath.Join(i.opts.LocalBinaryDir, binaryName)
	if _, err := os.Stat(localBinary); err != nil {
		return fmt.Errorf("local binary not found: %s: %w", localBinary, err)
	}

	// Ensure the remote directory exists.
	remoteDir := dirOf(binPath)
	if err := i.ensureRemoteDir(host, remoteDir); err != nil {
		return fmt.Errorf("create remote directory: %w", err)
	}

	i.logger.Info("copying binary to remote", "local", localBinary, "remote", binPath)
	if err := SCP(localBinary, host, binPath); err != nil {
		return fmt.Errorf("scp binary: %w", err)
	}

	// Step 5 (partial): Set binary permissions.
	if err := SetRemotePerms(host, binPath, "0755"); err != nil {
		return fmt.Errorf("set binary permissions: %w", err)
	}

	// Step 3: SCP the token file.
	const remoteTokenDir = "~/.config/clipbridge"
	const remoteTokenPath = "~/.config/clipbridge/token"
	if err := i.ensureRemoteDir(host, remoteTokenDir); err != nil {
		return fmt.Errorf("create remote token directory: %w", err)
	}

	i.logger.Info("copying token to remote", "local", i.opts.TokenPath, "remote", remoteTokenPath)
	if err := SCP(i.opts.TokenPath, host, remoteTokenPath); err != nil {
		return fmt.Errorf("scp token: %w", err)
	}

	// Step 5 (partial): Set token and directory permissions.
	if err := SetRemotePerms(host, remoteTokenDir, "0700"); err != nil {
		return fmt.Errorf("set token directory permissions: %w", err)
	}
	if err := SetRemotePerms(host, remoteTokenPath, "0600"); err != nil {
		return fmt.Errorf("set token permissions: %w", err)
	}

	// Step 6: Verify token file permissions on remote.
	if err := i.verifyTokenPerms(host, remoteTokenPath); err != nil {
		return fmt.Errorf("token permission verification failed: %w", err)
	}

	// Step 4: SCP the skill file (paste.md) to ~/.claude/commands/paste.md.
	const skillDir = "~/.claude/commands"
	const skillRemotePath = "~/.claude/commands/paste.md"
	if err := i.ensureRemoteDir(host, skillDir); err != nil {
		return fmt.Errorf("create remote skill directory: %w", err)
	}

	i.logger.Info("copying skill file to remote", "local", i.opts.SkillPath, "remote", skillRemotePath)
	if err := SCP(i.opts.SkillPath, host, skillRemotePath); err != nil {
		return fmt.Errorf("scp skill file: %w", err)
	}

	// Step 7: If not --termius: edit ~/.ssh/config to add RemoteForward.
	if !i.opts.Termius {
		if err := i.addSSHConfig(i.opts.Alias, port); err != nil {
			return fmt.Errorf("edit ssh config: %w", err)
		}
	} else {
		// For --termius mode: print the RemoteForward values user should
		// paste into Termius.
		if err := PrintTermiusInstructions(i.opts.Alias, port); err != nil {
			return fmt.Errorf("print termius instructions: %w", err)
		}
	}

	i.logger.Info("installation complete", "host", host, "alias", i.opts.Alias)
	return nil
}

// Uninstall reverses the installation.
// Steps:
//  1. SSH: rm the remote binary
//  2. SSH: rm the remote token
//  3. SSH: rm the skill file
//  4. If not --termius: edit ~/.ssh/config to remove RemoteForward
func (i *Installer) Uninstall() error {
	host := i.sshHost()
	binPath := i.binPath()
	port := i.port()

	// Step 1: Remove the remote binary.
	i.logger.Info("removing remote binary", "host", host, "path", binPath)
	if _, _, err := RunSSH(host, "rm", "-f", binPath); err != nil {
		i.logger.Warn("failed to remove remote binary", "error", err)
	}

	// Step 2: Remove the remote token.
	const remoteTokenPath = "~/.config/clipbridge/token"
	i.logger.Info("removing remote token", "host", host, "path", remoteTokenPath)
	if _, _, err := RunSSH(host, "rm", "-f", remoteTokenPath); err != nil {
		i.logger.Warn("failed to remove remote token", "error", err)
	}

	// Step 3: Remove the skill file.
	const skillRemotePath = "~/.claude/commands/paste.md"
	i.logger.Info("removing remote skill file", "host", host, "path", skillRemotePath)
	if _, _, err := RunSSH(host, "rm", "-f", skillRemotePath); err != nil {
		i.logger.Warn("failed to remove remote skill file", "error", err)
	}

	// Step 4: If not --termius: edit ~/.ssh/config to remove RemoteForward.
	if !i.opts.Termius {
		if err := i.removeSSHConfig(i.opts.Alias, port); err != nil {
			i.logger.Warn("failed to remove ssh config entry", "error", err)
		}
	}

	i.logger.Info("uninstallation complete", "host", host, "alias", i.opts.Alias)
	return nil
}

// installTokenOnly handles the --update-token-only path.
func (i *Installer) installTokenOnly(host string) error {
	const remoteTokenDir = "~/.config/clipbridge"
	const remoteTokenPath = "~/.config/clipbridge/token"

	if err := i.ensureRemoteDir(host, remoteTokenDir); err != nil {
		return fmt.Errorf("create remote token directory: %w", err)
	}

	i.logger.Info("copying token to remote (update only)", "local", i.opts.TokenPath, "remote", remoteTokenPath)
	if err := SCP(i.opts.TokenPath, host, remoteTokenPath); err != nil {
		return fmt.Errorf("scp token: %w", err)
	}

	if err := SetRemotePerms(host, remoteTokenDir, "0700"); err != nil {
		return fmt.Errorf("set token directory permissions: %w", err)
	}
	if err := SetRemotePerms(host, remoteTokenPath, "0600"); err != nil {
		return fmt.Errorf("set token permissions: %w", err)
	}

	// Verify token file permissions on remote.
	if err := i.verifyTokenPerms(host, remoteTokenPath); err != nil {
		return fmt.Errorf("token permission verification failed: %w", err)
	}

	i.logger.Info("token update complete", "host", host)
	return nil
}

// sshHost returns the SSH host string, optionally including the user.
func (i *Installer) sshHost() string {
	if i.opts.User != "" {
		return i.opts.User + "@" + i.opts.Host
	}
	return i.opts.Host
}

// port returns the configured port or the default.
func (i *Installer) port() int {
	if i.opts.Port != 0 {
		return i.opts.Port
	}
	return defaultPort
}

// binPath returns the configured remote binary path or the default.
func (i *Installer) binPath() string {
	if i.opts.RemotePath != "" {
		return i.opts.RemotePath
	}
	return defaultRemoteBinPath
}

// ensureRemoteDir creates a directory on the remote host if it doesn't exist.
func (i *Installer) ensureRemoteDir(host, dir string) error {
	if _, _, err := RunSSH(host, "mkdir", "-p", dir); err != nil {
		return fmt.Errorf("mkdir -p %s on %s: %w", dir, host, err)
	}
	return nil
}

// verifyTokenPerms checks that the token file on the remote host has
// exactly mode 0600. It aborts the installation if the permissions
// are incorrect.
func (i *Installer) verifyTokenPerms(host, path string) error {
	stdout, _, err := RunSSH(host, "stat", "-c", "%a", path)
	if err != nil {
		return fmt.Errorf("verify token perms: %w", err)
	}
	perm := strings.TrimSpace(stdout)
	if perm != "600" {
		return fmt.Errorf("token file has insecure permissions %s on remote, expected 600 — aborting", perm)
	}
	return nil
}

// addSSHConfig edits ~/.ssh/config to add a RemoteForward entry for the alias.
func (i *Installer) addSSHConfig(alias string, port int) error {
	cfgPath := userSSHConfigPath()

	content, err := os.ReadFile(cfgPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read ssh config: %w", err)
		}
		content = []byte{}
	}

	cfg, err := sshconfig.Parse(string(content))
	if err != nil {
		return fmt.Errorf("parse ssh config: %w", err)
	}

	if err := sshconfig.AddRemoteForward(cfg, alias, port); err != nil {
		return fmt.Errorf("add remote forward: %w", err)
	}

	i.logger.Info("updating ssh config", "path", cfgPath)
	return sshconfig.WriteAtomic(cfgPath, cfg.Render())
}

// removeSSHConfig edits ~/.ssh/config to remove the RemoteForward entry for the alias.
func (i *Installer) removeSSHConfig(alias string, port int) error {
	cfgPath := userSSHConfigPath()

	content, err := os.ReadFile(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read ssh config: %w", err)
	}

	cfg, err := sshconfig.Parse(string(content))
	if err != nil {
		return fmt.Errorf("parse ssh config: %w", err)
	}

	if err := sshconfig.RemoveRemoteForward(cfg, alias, port); err != nil {
		return fmt.Errorf("remove remote forward: %w", err)
	}

	i.logger.Info("updating ssh config", "path", cfgPath)
	return sshconfig.WriteAtomic(cfgPath, cfg.Render())
}

// dirOf extracts the directory portion of a path.
// For paths without a slash it returns ".".
func dirOf(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx == -1 {
		return "."
	}
	return path[:idx]
}

// userSSHConfigPath returns the path to the user's ~/.ssh/config file.
func userSSHConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.Getenv("HOME"), ".ssh", "config")
	}
	return filepath.Join(home, ".ssh", "config")
}
