//go:build linux

package service

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	systemdUnitName = "pastelocal.service"
)

// systemdUnitPath returns the full path to the systemd user unit file.
func systemdUnitPath() (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user", systemdUnitName), nil
}

// generateUnit generates the systemd unit file content.
func (s *Service) generateUnit() string {
	home, err := homeDir()
	if err != nil {
		home = "%h" // fall back to systemd specifier
	}

	execStart := fmt.Sprintf("%s --config %s", s.BinaryPath, s.ConfigPath)
	// If we have the home dir, substitute absolute paths
	if home != "%h" {
		execStart = fmt.Sprintf("%s/.local/bin/pastelocald --config %s/.config/pastelocal/config.toml", home, home)
	}

	return fmt.Sprintf(`[Unit]
Description=pastelocal clipboard daemon
After=network.target

[Service]
Type=simple
ExecStart=%s
Restart=on-failure
RestartSec=10

[Install]
WantedBy=default.target
`, execStart)
}

// installSystemd writes the unit file, reloads the daemon, and enables the service.
func (s *Service) installSystemd() error {
	unitPath, err := systemdUnitPath()
	if err != nil {
		return err
	}

	// Ensure the systemd user directory exists.
	if err := os.MkdirAll(filepath.Dir(unitPath), 0755); err != nil {
		return fmt.Errorf("creating systemd user directory: %w", err)
	}

	content := s.generateUnit()
	if err := os.WriteFile(unitPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing unit file: %w", err)
	}

	// Reload daemon.
	cmd := exec.Command("systemctl", "--user", "daemon-reload")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w: %s", err, out)
	}

	// Enable the service.
	cmd = exec.Command("systemctl", "--user", "enable", systemdUnitName)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl enable: %w: %s", err, out)
	}

	return nil
}

// uninstallSystemd disables, stops, removes the unit file, and reloads the daemon.
func (s *Service) uninstallSystemd() error {
	// Disable the service.
	cmd := exec.Command("systemctl", "--user", "disable", systemdUnitName)
	_, _ = cmd.CombinedOutput()

	// Stop the service.
	cmd = exec.Command("systemctl", "--user", "stop", systemdUnitName)
	_, _ = cmd.CombinedOutput()

	// Remove the unit file.
	unitPath, err := systemdUnitPath()
	if err != nil {
		return err
	}
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing unit file: %w", err)
	}

	// Reload daemon to pick up the removed file.
	cmd = exec.Command("systemctl", "--user", "daemon-reload")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w: %s", err, out)
	}

	return nil
}

// startSystemd starts the service via systemctl --user start.
func (s *Service) startSystemd() error {
	cmd := exec.Command("systemctl", "--user", "start", systemdUnitName)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl start: %w: %s", err, out)
	}
	return nil
}

// stopSystemd stops the service via systemctl --user stop.
func (s *Service) stopSystemd() error {
	cmd := exec.Command("systemctl", "--user", "stop", systemdUnitName)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl stop: %w: %s", err, out)
	}
	return nil
}

// restartSystemd restarts the service via systemctl --user restart.
func (s *Service) restartSystemd() error {
	cmd := exec.Command("systemctl", "--user", "restart", systemdUnitName)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl restart: %w: %s", err, out)
	}
	return nil
}

// statusSystemd reports whether the service is running. It parses the
// output of systemctl --user is-active to determine status and uses
// systemctl --user show to extract the MainPID.
func (s *Service) statusSystemd() (running bool, pid int, err error) {
	// Check if service is active.
	cmd := exec.Command("systemctl", "--user", "is-active", systemdUnitName)
	out, _ := cmd.Output()
	state := strings.TrimSpace(string(out))

	if state != "active" {
		return false, 0, nil
	}

	// Get MainPID.
	cmd = exec.Command("systemctl", "--user", "show", systemdUnitName, "--property=MainPID")
	out, showErr := cmd.Output()
	if showErr != nil {
		return true, 0, nil
	}

	line := strings.TrimSpace(string(out))
	parts := strings.SplitN(line, "=", 2)
	if len(parts) == 2 {
		p, parseErr := strconv.Atoi(parts[1])
		if parseErr == nil && p > 0 {
			return true, p, nil
		}
	}

	return true, 0, nil
}

// homeDir returns the current user's home directory (linux variant).
func homeDir() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("getting current user: %w", err)
	}
	return u.HomeDir, nil
}
