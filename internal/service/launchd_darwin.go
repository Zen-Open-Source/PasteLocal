//go:build darwin

package service

import (
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
)

const (
	launchdLabel    = "com.clipbridge.daemon"
	launchdPlistName = "com.clipbridge.daemon.plist"
)

// plistDoc represents the top-level XML document for a launchd plist.
type plistDoc struct {
	XMLName xml.Name    `xml:"plist"`
	Version string      `xml:"version,attr"`
	Dict    plistDict   `xml:"dict"`
}

// plistDict represents a <dict> element in the plist.
type plistDict struct {
	Keys   []string      `xml:"key"`
	Values []interface{} `xml:",any"`
}

// plistString represents a <string> element.
type plistString struct {
	XMLName xml.Name `xml:"string"`
	Value   string   `xml:",chardata"`
}

// plistTrue represents a <true/> element.
type plistTrue struct {
	XMLName xml.Name `xml:"true"`
}

// plistInteger represents a <integer> element.
type plistInteger struct {
	XMLName xml.Name `xml:"integer"`
	Value   int      `xml:",chardata"`
}

// plistArray represents an <array> element.
type plistArray struct {
	XMLName xml.Name    `xml:"array"`
	Strings []plistString `xml:"string"`
}

// launchdPlistPath returns the full path to the plist file.
func launchdPlistPath() (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchdPlistName), nil
}

// logPath returns the path for the given log file name.
func logPath(name string) (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Logs", name), nil
}

// homeDir returns the current user's home directory.
func homeDir() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("getting current user: %w", err)
	}
	return u.HomeDir, nil
}

// generatePlist generates the launchd plist content for the service.
func (s *Service) generatePlist() ([]byte, error) {
	stdoutPath, err := logPath("clipbridge.log")
	if err != nil {
		return nil, err
	}
	stderrPath, err := logPath("clipbridge.err")
	if err != nil {
		return nil, err
	}

	doc := plistDoc{
		Version: "1.0",
		Dict: plistDict{
			Keys: []string{
				"Label",
				"RunAtLoad",
				"KeepAlive",
				"ThrottleInterval",
				"ProgramArguments",
				"StandardOutPath",
				"StandardErrorPath",
			},
			Values: []interface{}{
				plistString{Value: launchdLabel},
				plistTrue{},
				plistTrue{},
				plistInteger{Value: 10},
				plistArray{
					Strings: []plistString{
						{Value: s.BinaryPath},
						{Value: "--config"},
						{Value: s.ConfigPath},
					},
				},
				plistString{Value: stdoutPath},
				plistString{Value: stderrPath},
			},
		},
	}

	out, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling plist XML: %w", err)
	}
	return append([]byte(xml.Header), out...), nil
}

// installLaunchd writes the plist file and loads it via launchctl.
func (s *Service) installLaunchd() error {
	content, err := s.generatePlist()
	if err != nil {
		return err
	}

	plistPath, err := launchdPlistPath()
	if err != nil {
		return err
	}

	// Ensure the LaunchAgents directory exists.
	if err := os.MkdirAll(filepath.Dir(plistPath), 0755); err != nil {
		return fmt.Errorf("creating LaunchAgents directory: %w", err)
	}

	if err := os.WriteFile(plistPath, content, 0644); err != nil {
		return fmt.Errorf("writing plist file: %w", err)
	}

	cmd := exec.Command("launchctl", "load", plistPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl load: %w: %s", err, out)
	}
	return nil
}

// uninstallLaunchd unloads and removes the plist file.
func (s *Service) uninstallLaunchd() error {
	plistPath, err := launchdPlistPath()
	if err != nil {
		return err
	}

	// Try to unload; ignore error if plist is not loaded.
	cmd := exec.Command("launchctl", "unload", plistPath)
	_, _ = cmd.CombinedOutput()

	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing plist file: %w", err)
	}
	return nil
}

// startLaunchd starts the service via launchctl load.
func (s *Service) startLaunchd() error {
	plistPath, err := launchdPlistPath()
	if err != nil {
		return err
	}

	cmd := exec.Command("launchctl", "load", plistPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl load: %w: %s", err, out)
	}
	return nil
}

// stopLaunchd stops the service via launchctl unload.
func (s *Service) stopLaunchd() error {
	plistPath, err := launchdPlistPath()
	if err != nil {
		return err
	}

	cmd := exec.Command("launchctl", "unload", plistPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl unload: %w: %s", err, out)
	}
	return nil
}

// restartLaunchd restarts the service by unloading and loading.
func (s *Service) restartLaunchd() error {
	if err := s.stopLaunchd(); err != nil {
		return err
	}
	return s.startLaunchd()
}

// statusLaunchd reports whether the service is running. It parses the
// output of launchctl print to extract the PID.
func (s *Service) statusLaunchd() (running bool, pid int, err error) {
	uid := os.Getuid()
	target := fmt.Sprintf("gui/%d/%s", uid, launchdLabel)

	cmd := exec.Command("launchctl", "print", target)
	out, execErr := cmd.CombinedOutput()
	if execErr != nil {
		// If print fails, the service is not loaded/running.
		return false, 0, nil
	}

	// Parse PID from output. Look for "pid = <number>".
	re := regexp.MustCompile(`pid\s*=\s*(\d+)`)
	matches := re.FindStringSubmatch(string(out))
	if len(matches) >= 2 {
		p, parseErr := strconv.Atoi(matches[1])
		if parseErr == nil {
			return true, p, nil
		}
	}

	// Service is loaded but we couldn't parse PID; it might still be running.
	return true, 0, nil
}
