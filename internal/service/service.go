package service

import (
	"fmt"
	"runtime"
)

// Service represents a system service configuration for clipbridged.
type Service struct {
	BinaryPath string // path to clipbridged binary
	ConfigPath string // path to config.toml
	Port       int
}

// New creates a new Service with the given binary path, config path, and port.
func New(binaryPath, configPath string, port int) *Service {
	return &Service{
		BinaryPath: binaryPath,
		ConfigPath: configPath,
		Port:       port,
	}
}

// Install detects the current platform and installs the appropriate
// service unit (launchd on macOS, systemd on Linux).
func (s *Service) Install() error {
	switch runtime.GOOS {
	case "darwin":
		return s.installLaunchd()
	case "linux":
		return s.installSystemd()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// Uninstall removes the service unit from the system.
func (s *Service) Uninstall() error {
	switch runtime.GOOS {
	case "darwin":
		return s.uninstallLaunchd()
	case "linux":
		return s.uninstallSystemd()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// Start starts the service.
func (s *Service) Start() error {
	switch runtime.GOOS {
	case "darwin":
		return s.startLaunchd()
	case "linux":
		return s.startSystemd()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// Stop stops the service.
func (s *Service) Stop() error {
	switch runtime.GOOS {
	case "darwin":
		return s.stopLaunchd()
	case "linux":
		return s.stopSystemd()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// Restart restarts the service.
func (s *Service) Restart() error {
	switch runtime.GOOS {
	case "darwin":
		return s.restartLaunchd()
	case "linux":
		return s.restartSystemd()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// Status reports whether the service is running. If running is true, pid
// contains the process ID.
func (s *Service) Status() (running bool, pid int, err error) {
	switch runtime.GOOS {
	case "darwin":
		return s.statusLaunchd()
	case "linux":
		return s.statusSystemd()
	default:
		return false, 0, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}
