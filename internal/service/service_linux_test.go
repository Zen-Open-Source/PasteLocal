//go:build linux

package service

import (
	"os"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	s := New("/usr/local/bin/pastelocald", "/etc/pastelocal/config.toml", 8080)
	if s.BinaryPath != "/usr/local/bin/pastelocald" {
		t.Errorf("BinaryPath = %q, want /usr/local/bin/pastelocald", s.BinaryPath)
	}
	if s.ConfigPath != "/etc/pastelocal/config.toml" {
		t.Errorf("ConfigPath = %q, want /etc/pastelocal/config.toml", s.ConfigPath)
	}
	if s.Port != 8080 {
		t.Errorf("Port = %d, want 8080", s.Port)
	}
}

func TestSystemdUnitGeneration(t *testing.T) {
	s := New("/home/user/.local/bin/pastelocald", "/home/user/.config/pastelocal/config.toml", 8080)
	content := s.generateUnit()

	// Verify required sections.
	sections := []string{"[Unit]", "[Service]", "[Install]"}
	for _, section := range sections {
		if !strings.Contains(content, section) {
			t.Errorf("unit missing section: %s", section)
		}
	}

	// Verify Unit fields.
	if !strings.Contains(content, "Description=pastelocal clipboard daemon") {
		t.Error("unit missing Description")
	}
	if !strings.Contains(content, "After=network.target") {
		t.Error("unit missing After=network.target")
	}

	// Verify Service fields.
	if !strings.Contains(content, "Type=simple") {
		t.Error("unit missing Type=simple")
	}
	if !strings.Contains(content, "Restart=on-failure") {
		t.Error("unit missing Restart=on-failure")
	}
	if !strings.Contains(content, "RestartSec=10") {
		t.Error("unit missing RestartSec=10")
	}
	if !strings.Contains(content, "ExecStart=") {
		t.Error("unit missing ExecStart")
	}

	// Verify Install fields.
	if !strings.Contains(content, "WantedBy=default.target") {
		t.Error("unit missing WantedBy=default.target")
	}
}

func TestSystemdUnitPath(t *testing.T) {
	path, err := systemdUnitPath()
	if err != nil {
		t.Fatalf("systemdUnitPath() error: %v", err)
	}

	home, _ := homeDir()
	expected := home + "/.config/systemd/user/pastelocal.service"
	if path != expected {
		t.Errorf("systemdUnitPath() = %q, want %q", path, expected)
	}
}

func TestInstallUninstallExist(t *testing.T) {
	s := New("/usr/local/bin/pastelocald", "/etc/pastelocal/config.toml", 8080)

	// Verify Install and Uninstall methods exist on the Service type.
	_ = s.Install
	_ = s.Uninstall
	_ = s.Start
	_ = s.Stop
	_ = s.Restart
	_ = s.Status
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
