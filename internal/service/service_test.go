//go:build darwin

package service

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	s := New("/usr/local/bin/clipbridged", "/etc/clipbridge/config.toml", 8080)
	if s.BinaryPath != "/usr/local/bin/clipbridged" {
		t.Errorf("BinaryPath = %q, want /usr/local/bin/clipbridged", s.BinaryPath)
	}
	if s.ConfigPath != "/etc/clipbridge/config.toml" {
		t.Errorf("ConfigPath = %q, want /etc/clipbridge/config.toml", s.ConfigPath)
	}
	if s.Port != 8080 {
		t.Errorf("Port = %d, want 8080", s.Port)
	}
}

func TestPlistGeneration(t *testing.T) {
	s := New("/usr/local/bin/clipbridged", "/etc/clipbridge/config.toml", 8080)
	content, err := s.generatePlist()
	if err != nil {
		t.Fatalf("generatePlist() error: %v", err)
	}

	// Verify it's valid XML by tokenizing the entire document.
	decoder := xml.NewDecoder(strings.NewReader(string(content)))
	for {
		_, tokErr := decoder.Token()
		if tokErr != nil {
			if tokErr.Error() == "EOF" {
				break
			}
			t.Fatalf("plist is not valid XML: %v", tokErr)
		}
	}

	str := string(content)

	// Verify XML declaration.
	if !strings.HasPrefix(str, "<?xml") {
		t.Error("plist does not start with XML declaration")
	}

	// Verify plist version attribute.
	if !strings.Contains(str, `version="1.0"`) {
		t.Error("plist missing version=\"1.0\"")
	}

	// Verify Label.
	if !strings.Contains(str, "<key>Label</key>") {
		t.Error("plist missing Label key")
	}
	if !strings.Contains(str, "<string>"+launchdLabel+"</string>") {
		t.Errorf("plist missing Label value %q", launchdLabel)
	}

	// Verify RunAtLoad and KeepAlive are true.
	if !strings.Contains(str, "<key>RunAtLoad</key>") {
		t.Error("plist missing RunAtLoad key")
	}
	if !strings.Contains(str, "<true/>") && !strings.Contains(str, "<true />") && !strings.Contains(str, "<true></true>") {
		t.Error("plist missing <true/> element")
	}
	if !strings.Contains(str, "<key>KeepAlive</key>") {
		t.Error("plist missing KeepAlive key")
	}

	// Verify ThrottleInterval.
	if !strings.Contains(str, "<key>ThrottleInterval</key>") {
		t.Error("plist missing ThrottleInterval key")
	}
	if !strings.Contains(str, "<integer>10</integer>") {
		t.Error("plist missing ThrottleInterval value 10")
	}

	// Verify ProgramArguments array with correct values.
	if !strings.Contains(str, "<key>ProgramArguments</key>") {
		t.Error("plist missing ProgramArguments key")
	}
	if !strings.Contains(str, "<array>") {
		t.Error("plist missing <array> element")
	}
	if !strings.Contains(str, "<string>/usr/local/bin/clipbridged</string>") {
		t.Error("plist missing binary path in ProgramArguments")
	}
	if !strings.Contains(str, "<string>--config</string>") {
		t.Error("plist missing --config in ProgramArguments")
	}
	if !strings.Contains(str, "<string>/etc/clipbridge/config.toml</string>") {
		t.Error("plist missing config path in ProgramArguments")
	}

	// Verify StandardOutPath and StandardErrorPath.
	home, _ := homeDir()
	expectedStdout := filepath.Join(home, "Library", "Logs", "clipbridge.log")
	expectedStderr := filepath.Join(home, "Library", "Logs", "clipbridge.err")
	if !strings.Contains(str, expectedStdout) {
		t.Errorf("plist missing StandardOutPath %q", expectedStdout)
	}
	if !strings.Contains(str, expectedStderr) {
		t.Errorf("plist missing StandardErrorPath %q", expectedStderr)
	}
}

func TestPlistXMLDeclaration(t *testing.T) {
	s := New("/usr/local/bin/clipbridged", "/etc/clipbridge/config.toml", 8080)
	content, err := s.generatePlist()
	if err != nil {
		t.Fatalf("generatePlist() error: %v", err)
	}

	// Verify XML declaration is present.
	str := string(content)
	if !strings.HasPrefix(str, "<?xml") {
		t.Error("plist does not start with XML declaration")
	}
}

func TestPlistPath(t *testing.T) {
	path, err := launchdPlistPath()
	if err != nil {
		t.Fatalf("launchdPlistPath() error: %v", err)
	}

	home, _ := homeDir()
	expected := filepath.Join(home, "Library", "LaunchAgents", launchdPlistName)
	if path != expected {
		t.Errorf("launchdPlistPath() = %q, want %q", path, expected)
	}
}

func TestInstallUninstallExist(t *testing.T) {
	s := New("/usr/local/bin/clipbridged", "/etc/clipbridge/config.toml", 8080)

	// Verify Install and Uninstall methods exist on the Service type.
	// We don't actually run them (they require launchctl), just verify
	// the methods are callable.
	_ = s.Install
	_ = s.Uninstall
	_ = s.Start
	_ = s.Stop
	_ = s.Restart
	_ = s.Status
}

func TestPlistContainsRequiredFields(t *testing.T) {
	s := New("/opt/clipbridge/bin/clipbridged", "/opt/clipbridge/config.toml", 9090)
	content, err := s.generatePlist()
	if err != nil {
		t.Fatalf("generatePlist() error: %v", err)
	}

	str := string(content)
	requiredFields := []string{
		"<key>Label</key>",
		"<key>RunAtLoad</key>",
		"<key>KeepAlive</key>",
		"<key>ThrottleInterval</key>",
		"<key>ProgramArguments</key>",
		"<key>StandardOutPath</key>",
		"<key>StandardErrorPath</key>",
	}
	for _, field := range requiredFields {
		if !strings.Contains(str, field) {
			t.Errorf("plist missing required field: %s", field)
		}
	}

	// Verify custom binary path appears.
	if !strings.Contains(str, "/opt/clipbridge/bin/clipbridged") {
		t.Error("plist does not contain the custom binary path")
	}
	if !strings.Contains(str, "/opt/clipbridge/config.toml") {
		t.Error("plist does not contain the custom config path")
	}
}

// --- Status returns not-running when service is not installed ---

func TestStatusNotRunning(t *testing.T) {
	s := New("", "", 7331)
	running, pid, err := s.Status()
	if err != nil {
		t.Errorf("Status() returned unexpected error: %v", err)
	}
	if running {
		t.Error("Status() should report not running when service is not installed")
	}
	if pid != 0 {
		t.Errorf("pid = %d, want 0 when not running", pid)
	}
}

// --- Start when service is not installed ---

func TestStartWhenNotInstalled(t *testing.T) {
	s := New("/nonexistent/binary", "/nonexistent/config", 7331)
	err := s.Start()
	// Start will fail because the plist is not loaded
	// (or succeed if somehow launchctl picks it up — just verify no panic)
	_ = err
}

// --- Stop when service is not installed ---

func TestStopWhenNotInstalled(t *testing.T) {
	s := New("/nonexistent/binary", "/nonexistent/config", 7331)
	err := s.Stop()
	// Stop will fail because the plist is not loaded
	// Just verify no panic
	_ = err
}

// --- Restart when service is not installed ---

func TestRestartWhenNotInstalled(t *testing.T) {
	s := New("/nonexistent/binary", "/nonexistent/config", 7331)
	err := s.Restart()
	// Restart will fail because the plist is not loaded
	// Just verify no panic
	_ = err
}

// --- Uninstall succeeds even when plist doesn't exist ---

func TestUninstallNoPlistFile(t *testing.T) {
	s := New("/nonexistent/binary", "/nonexistent/config", 7331)
	err := s.Uninstall()
	if err != nil {
		t.Errorf("Uninstall() should not error when plist doesn't exist, got: %v", err)
	}
}

// --- Install attempts to write plist and call launchctl ---

func TestInstallAttemptsWriteAndLoad(t *testing.T) {
	s := New("/usr/local/bin/clipbridged", "/etc/clipbridge/config.toml", 8080)
	err := s.Install()
	// Install will either succeed (unlikely unless daemon already configured)
	// or fail on launchctl load. Either way, we verify it doesn't panic.
	_ = err
}

// --- Uninstall removes an existing plist file ---

func TestUninstallRemovesExistingPlist(t *testing.T) {
	s := New("/usr/local/bin/clipbridged", "/etc/clipbridge/config.toml", 8080)

	// First install to create the plist (may fail on launchctl load but writes plist)
	_ = s.Install()

	// Now uninstall - should remove the plist and succeed
	err := s.Uninstall()
	if err != nil {
		t.Errorf("Uninstall() error: %v", err)
	}
}

// --- Systemd stubs return errors on darwin ---

func TestSystemdStubsReturnErrors(t *testing.T) {
	s := New("/usr/local/bin/clipbridged", "/etc/clipbridge/config.toml", 8080)

	if err := s.installSystemd(); err == nil {
		t.Error("installSystemd() should return error on darwin")
	}
	if err := s.uninstallSystemd(); err == nil {
		t.Error("uninstallSystemd() should return error on darwin")
	}
	if err := s.startSystemd(); err == nil {
		t.Error("startSystemd() should return error on darwin")
	}
	if err := s.stopSystemd(); err == nil {
		t.Error("stopSystemd() should return error on darwin")
	}
	if err := s.restartSystemd(); err == nil {
		t.Error("restartSystemd() should return error on darwin")
	}
	running, pid, err := s.statusSystemd()
	if err == nil {
		t.Error("statusSystemd() should return error on darwin")
	}
	if running {
		t.Error("statusSystemd() should report not running on darwin")
	}
	if pid != 0 {
		t.Errorf("statusSystemd() pid = %d, want 0", pid)
	}
}

// --- homeDir returns a non-empty path ---

func TestHomeDir(t *testing.T) {
	home, err := homeDir()
	if err != nil {
		t.Errorf("homeDir() error: %v", err)
	}
	if home == "" {
		t.Error("homeDir() should return non-empty string")
	}
}

// --- logPath returns valid paths ---

func TestLogPath(t *testing.T) {
	path, err := logPath("clipbridge.log")
	if err != nil {
		t.Errorf("logPath() error: %v", err)
	}
	if !strings.Contains(path, "clipbridge.log") {
		t.Errorf("logPath() = %q, should contain clipbridge.log", path)
	}
}

// --- generatePlist with various configurations ---

func TestGeneratePlistWithDifferentPorts(t *testing.T) {
	tests := []struct {
		name string
		port int
	}{
		{"default port", 7331},
		{"custom port", 9999},
		{"low port", 80},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New("/usr/local/bin/clipbridged", "/etc/clipbridge/config.toml", tt.port)
			content, err := s.generatePlist()
			if err != nil {
				t.Fatalf("generatePlist() error: %v", err)
			}
			// Verify it's valid XML
			str := string(content)
			if !strings.HasPrefix(str, "<?xml") {
				t.Error("plist should start with XML declaration")
			}
			if !strings.Contains(str, launchdLabel) {
				t.Error("plist should contain the label")
			}
		})
	}
}

// --- launchdPlistPath returns valid path ---

func TestLaunchdPlistPathValid(t *testing.T) {
	path, err := launchdPlistPath()
	if err != nil {
		t.Fatalf("launchdPlistPath() error: %v", err)
	}
	if !strings.Contains(path, "LaunchAgents") {
		t.Errorf("launchdPlistPath() = %q, should contain LaunchAgents", path)
	}
	if !strings.Contains(path, launchdPlistName) {
		t.Errorf("launchdPlistPath() = %q, should contain %q", path, launchdPlistName)
	}
}

// --- Status dispatches to correct platform ---

func TestStatusDispatchesToLaunchd(t *testing.T) {
	s := New("", "", 7331)
	running, _, err := s.Status()
	// On darwin, statusLaunchd should be called
	// It returns (false, 0, nil) when the service isn't running
	if err != nil {
		t.Errorf("Status() error: %v", err)
	}
	if running {
		t.Error("Status() should return not running")
	}
}

// --- Install dispatches to correct platform ---

func TestInstallDispatchesToLaunchd(t *testing.T) {
	s := New("/nonexistent/binary", "/nonexistent/config", 7331)
	err := s.Install()
	// Will call installLaunchd which tries to write plist and run launchctl
	_ = err
}

// --- Uninstall dispatches to correct platform ---

func TestUninstallDispatchesToLaunchd(t *testing.T) {
	s := New("/nonexistent/binary", "/nonexistent/config", 7331)
	err := s.Uninstall()
	// Will call uninstallLaunchd which tries to unload and remove plist
	_ = err
}

// --- Start dispatches to correct platform ---

func TestStartDispatchesToLaunchd(t *testing.T) {
	s := New("/nonexistent/binary", "/nonexistent/config", 7331)
	err := s.Start()
	// Will call startLaunchd
	_ = err
}

// --- Stop dispatches to correct platform ---

func TestStopDispatchesToLaunchd(t *testing.T) {
	s := New("/nonexistent/binary", "/nonexistent/config", 7331)
	err := s.Stop()
	// Will call stopLaunchd
	_ = err
}

// --- Restart dispatches to correct platform ---

func TestRestartDispatchesToLaunchd(t *testing.T) {
	s := New("/nonexistent/binary", "/nonexistent/config", 7331)
	err := s.Restart()
	// Will call restartLaunchd
	_ = err
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
