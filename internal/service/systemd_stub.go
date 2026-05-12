//go:build darwin

package service

import "fmt"

// Stubs for systemd methods on non-linux platforms.

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
