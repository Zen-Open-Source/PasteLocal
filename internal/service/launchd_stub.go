//go:build linux

package service

import "fmt"

// Stubs for launchd methods on non-darwin platforms.

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
