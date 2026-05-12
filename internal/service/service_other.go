//go:build !darwin && !linux

package service

import "fmt"

// Stubs for all service methods on unsupported platforms.

func (s *Service) installLaunchd() error {
	return fmt.Errorf("launchd is not available on this platform")
}

func (s *Service) uninstallLaunchd() error {
	return fmt.Errorf("launchd is not available on this platform")
}

func (s *Service) startLaunchd() error {
	return fmt.Errorf("launchd is not available on this platform")
}

func (s *Service) stopLaunchd() error {
	return fmt.Errorf("launchd is not available on this platform")
}

func (s *Service) restartLaunchd() error {
	return fmt.Errorf("launchd is not available on this platform")
}

func (s *Service) statusLaunchd() (bool, int, error) {
	return false, 0, fmt.Errorf("launchd is not available on this platform")
}

func (s *Service) installSystemd() error {
	return fmt.Errorf("systemd is not available on this platform")
}

func (s *Service) uninstallSystemd() error {
	return fmt.Errorf("systemd is not available on this platform")
}

func (s *Service) startSystemd() error {
	return fmt.Errorf("systemd is not available on this platform")
}

func (s *Service) stopSystemd() error {
	return fmt.Errorf("systemd is not available on this platform")
}

func (s *Service) restartSystemd() error {
	return fmt.Errorf("systemd is not available on this platform")
}

func (s *Service) statusSystemd() (bool, int, error) {
	return false, 0, fmt.Errorf("systemd is not available on this platform")
}
