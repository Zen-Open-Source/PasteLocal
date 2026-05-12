package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	if cfg.Port != DefaultPort {
		t.Errorf("Default().Port = %d, want %d", cfg.Port, DefaultPort)
	}
	if cfg.Transport != DefaultTransport {
		t.Errorf("Default().Transport = %q, want %q", cfg.Transport, DefaultTransport)
	}
	if cfg.LogLevel != DefaultLogLevel {
		t.Errorf("Default().LogLevel = %q, want %q", cfg.LogLevel, DefaultLogLevel)
	}
	if cfg.MaxImageBytes != DefaultMaxImageBytes {
		t.Errorf("Default().MaxImageBytes = %d, want %d", cfg.MaxImageBytes, DefaultMaxImageBytes)
	}
	if cfg.MaxInFlight != DefaultMaxInFlight {
		t.Errorf("Default().MaxInFlight = %d, want %d", cfg.MaxInFlight, DefaultMaxInFlight)
	}
	if cfg.RateLimitPerMinute != DefaultRateLimitPerMin {
		t.Errorf("Default().RateLimitPerMinute = %d, want %d", cfg.RateLimitPerMinute, DefaultRateLimitPerMin)
	}
	if cfg.Hosts == nil {
		t.Error("Default().Hosts should not be nil")
	}
	if len(cfg.Hosts) != 0 {
		t.Errorf("Default().Hosts should be empty, got %d entries", len(cfg.Hosts))
	}
}

func TestLoadNonExistentFile(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.toml")
	if err != nil {
		t.Fatalf("Load with non-existent file should not error: %v", err)
	}

	def := Default()
	if cfg.Port != def.Port {
		t.Errorf("Load(nonexistent).Port = %d, want %d", cfg.Port, def.Port)
	}
	if cfg.Transport != def.Transport {
		t.Errorf("Load(nonexistent).Transport = %q, want %q", cfg.Transport, def.Transport)
	}
}

func TestLoadWithValidTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
port = 9999
transport = "quic"
log_level = "debug"
max_image_bytes = 1048576
max_in_flight = 8
rate_limit_per_minute = 120
audit_log = "/var/log/clipbridge.log"

[macos]
pngpaste_path = "/usr/local/bin/pngpaste"

[linux]
clipboard_tool = "wl-paste"

[hosts.myserver]
added_at = 2024-01-15T10:30:00Z
remote_port = 22
remote_user = "deploy"
remote_path = "/home/deploy"
termius = true
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Port != 9999 {
		t.Errorf("Port = %d, want 9999", cfg.Port)
	}
	if cfg.Transport != "quic" {
		t.Errorf("Transport = %q, want %q", cfg.Transport, "quic")
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
	if cfg.MaxImageBytes != 1048576 {
		t.Errorf("MaxImageBytes = %d, want 1048576", cfg.MaxImageBytes)
	}
	if cfg.MaxInFlight != 8 {
		t.Errorf("MaxInFlight = %d, want 8", cfg.MaxInFlight)
	}
	if cfg.RateLimitPerMinute != 120 {
		t.Errorf("RateLimitPerMinute = %d, want 120", cfg.RateLimitPerMinute)
	}
	if cfg.AuditLog != "/var/log/clipbridge.log" {
		t.Errorf("AuditLog = %q, want %q", cfg.AuditLog, "/var/log/clipbridge.log")
	}
	if cfg.MacOS.PngpastePath != "/usr/local/bin/pngpaste" {
		t.Errorf("MacOS.PngpastePath = %q, want %q", cfg.MacOS.PngpastePath, "/usr/local/bin/pngpaste")
	}
	if cfg.Linux.ClipboardTool != "wl-paste" {
		t.Errorf("Linux.ClipboardTool = %q, want %q", cfg.Linux.ClipboardTool, "wl-paste")
	}

	host, ok := cfg.Hosts["myserver"]
	if !ok {
		t.Fatal("Host 'myserver' not found")
	}
	if host.RemotePort != 22 {
		t.Errorf("Host.RemotePort = %d, want 22", host.RemotePort)
	}
	if host.RemoteUser != "deploy" {
		t.Errorf("Host.RemoteUser = %q, want %q", host.RemoteUser, "deploy")
	}
	if host.RemotePath != "/home/deploy" {
		t.Errorf("Host.RemotePath = %q, want %q", host.RemotePath, "/home/deploy")
	}
	if !host.Termius {
		t.Error("Host.Termius = false, want true")
	}
}

func TestLoadPartialTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
port = 8080
log_level = "warn"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Overridden values
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
	if cfg.LogLevel != "warn" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "warn")
	}

	// Missing fields should use defaults
	if cfg.Transport != DefaultTransport {
		t.Errorf("Transport = %q, want default %q", cfg.Transport, DefaultTransport)
	}
	if cfg.MaxImageBytes != DefaultMaxImageBytes {
		t.Errorf("MaxImageBytes = %d, want default %d", cfg.MaxImageBytes, DefaultMaxImageBytes)
	}
	if cfg.MaxInFlight != DefaultMaxInFlight {
		t.Errorf("MaxInFlight = %d, want default %d", cfg.MaxInFlight, DefaultMaxInFlight)
	}
	if cfg.RateLimitPerMinute != DefaultRateLimitPerMin {
		t.Errorf("RateLimitPerMinute = %d, want default %d", cfg.RateLimitPerMinute, DefaultRateLimitPerMin)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	original := Default()
	original.Port = 1234
	original.LogLevel = "debug"
	original.AuditLog = "/tmp/audit.log"
	original.MacOS = MacOSConfig{PngpastePath: "/opt/bin/pngpaste"}
	original.Linux = LinuxConfig{ClipboardTool: "xclip"}
	original.Hosts = map[string]Host{
		"server1": {
			AddedAt:    time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC),
			RemotePort:  22,
			RemoteUser: "admin",
			RemotePath: "/opt/app",
			Termius:    false,
		},
	}

	if err := Save(original, path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.Port != original.Port {
		t.Errorf("Port: got %d, want %d", loaded.Port, original.Port)
	}
	if loaded.LogLevel != original.LogLevel {
		t.Errorf("LogLevel: got %q, want %q", loaded.LogLevel, original.LogLevel)
	}
	if loaded.AuditLog != original.AuditLog {
		t.Errorf("AuditLog: got %q, want %q", loaded.AuditLog, original.AuditLog)
	}
	if loaded.MacOS.PngpastePath != original.MacOS.PngpastePath {
		t.Errorf("MacOS.PngpastePath: got %q, want %q", loaded.MacOS.PngpastePath, original.MacOS.PngpastePath)
	}
	if loaded.Linux.ClipboardTool != original.Linux.ClipboardTool {
		t.Errorf("Linux.ClipboardTool: got %q, want %q", loaded.Linux.ClipboardTool, original.Linux.ClipboardTool)
	}

	host, ok := loaded.Hosts["server1"]
	if !ok {
		t.Fatal("Host 'server1' not found")
	}
	if host.RemotePort != 22 {
		t.Errorf("Host.RemotePort = %d, want 22", host.RemotePort)
	}
	if host.RemoteUser != "admin" {
		t.Errorf("Host.RemoteUser = %q, want %q", host.RemoteUser, "admin")
	}
	if host.RemotePath != "/opt/app" {
		t.Errorf("Host.RemotePath = %q, want %q", host.RemotePath, "/opt/app")
	}
}

func TestAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := Default()
	cfg.Port = 5555

	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify the file exists at the target path
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("Config file should exist after Save")
	}

	// Verify no temp files are left behind
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("Failed to read directory: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "config.toml" {
			t.Errorf("Unexpected file in directory: %s", e.Name())
		}
	}

	// Verify content is valid
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load after atomic Save failed: %v", err)
	}
	if loaded.Port != 5555 {
		t.Errorf("Loaded Port = %d, want 5555", loaded.Port)
	}
}

func TestSaveCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	nestedDir := filepath.Join(dir, "sub", "nested")
	path := filepath.Join(nestedDir, "config.toml")

	cfg := Default()
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save with nested directory should succeed: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("Config file should exist after Save created directories")
	}
}

func TestEnvOverrides(t *testing.T) {
	// Set environment variables
	os.Setenv("CLIPBRIDGE_PORT", "9000")
	os.Setenv("CLIPBRIDGE_LOG_LEVEL", "trace")
	defer func() {
		os.Unsetenv("CLIPBRIDGE_PORT")
		os.Unsetenv("CLIPBRIDGE_LOG_LEVEL")
	}()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	// Create a config with specific values
	content := `
port = 8080
log_level = "warn"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Environment variables should override file values
	if cfg.Port != 9000 {
		t.Errorf("Port = %d, want 9000 (env override)", cfg.Port)
	}
	if cfg.LogLevel != "trace" {
		t.Errorf("LogLevel = %q, want %q (env override)", cfg.LogLevel, "trace")
	}
}

func TestEnvOverridesOnDefault(t *testing.T) {
	os.Setenv("CLIPBRIDGE_PORT", "7777")
	os.Setenv("CLIPBRIDGE_LOG_LEVEL", "error")
	defer func() {
		os.Unsetenv("CLIPBRIDGE_PORT")
		os.Unsetenv("CLIPBRIDGE_LOG_LEVEL")
	}()

	cfg, err := Load("/nonexistent/path/config.toml")
	if err != nil {
		t.Fatalf("Load with non-existent file should not error: %v", err)
	}

	if cfg.Port != 7777 {
		t.Errorf("Port = %d, want 7777 (env override)", cfg.Port)
	}
	if cfg.LogLevel != "error" {
		t.Errorf("LogLevel = %q, want %q (env override)", cfg.LogLevel, "error")
	}
}

func TestConfigDirEnvOverride(t *testing.T) {
	customDir := "/tmp/custom-clipbridge-config"
	os.Setenv("CLIPBRIDGE_CONFIG_DIR", customDir)
	defer os.Unsetenv("CLIPBRIDGE_CONFIG_DIR")

	got := ConfigDir()
	if got != customDir {
		t.Errorf("ConfigDir() = %q, want %q", got, customDir)
	}
}

func TestConfigDirDefault(t *testing.T) {
	os.Unsetenv("CLIPBRIDGE_CONFIG_DIR")

	got := ConfigDir()
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".config", "clipbridge")
	if got != expected {
		t.Errorf("ConfigDir() = %q, want %q", got, expected)
	}
}

func TestConfigPathExpansion(t *testing.T) {
	os.Unsetenv("CLIPBRIDGE_CONFIG_DIR")

	got := ConfigPath()
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".config", "clipbridge", DefaultConfigFile)
	if got != expected {
		t.Errorf("ConfigPath() = %q, want %q", got, expected)
	}
}

func TestHostAddRemove(t *testing.T) {
	cfg := Default()

	now := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)
	host := Host{
		AddedAt:    now,
		RemotePort: 22,
		RemoteUser: "user",
		RemotePath: "/home/user",
		Termius:    true,
	}

	cfg.AddHost("myhost", host)
	if len(cfg.Hosts) != 1 {
		t.Errorf("After AddHost: len(Hosts) = %d, want 1", len(cfg.Hosts))
	}

	got, ok := cfg.Hosts["myhost"]
	if !ok {
		t.Fatal("Host 'myhost' not found after AddHost")
	}
	if got.RemotePort != 22 {
		t.Errorf("Host.RemotePort = %d, want 22", got.RemotePort)
	}
	if got.RemoteUser != "user" {
		t.Errorf("Host.RemoteUser = %q, want %q", got.RemoteUser, "user")
	}
	if !got.Termius {
		t.Error("Host.Termius = false, want true")
	}

	cfg.RemoveHost("myhost")
	if len(cfg.Hosts) != 0 {
		t.Errorf("After RemoveHost: len(Hosts) = %d, want 0", len(cfg.Hosts))
	}

	// Removing non-existent host should not panic
	cfg.RemoveHost("nonexistent")
}

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()

	tests := []struct {
		input    string
		expected string
	}{
		{"~/path", filepath.Join(home, "path")},
		{"~", home},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
		{"", ""},
		{"~/", filepath.Join(home, "")},
	}

	for _, tt := range tests {
		got := expandHome(tt.input)
		if got != tt.expected {
			t.Errorf("expandHome(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestLoadInvalidTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	// Write invalid TOML content
	content := `
port = not_a_number
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Error("Load with invalid TOML should return an error")
	}
}

func TestAddHostNilMap(t *testing.T) {
	cfg := &Config{Hosts: nil}

	host := Host{
		AddedAt:    time.Now().UTC(),
		RemotePort: 22,
		RemoteUser: "testuser",
		RemotePath: "/home/testuser",
		Termius:    false,
	}

	cfg.AddHost("first", host)
	if cfg.Hosts == nil {
		t.Error("AddHost should initialize nil Hosts map")
	}
	if len(cfg.Hosts) != 1 {
		t.Errorf("After AddHost: len(Hosts) = %d, want 1", len(cfg.Hosts))
	}
}

func TestMultipleHostsAddRemove(t *testing.T) {
	cfg := Default()

	hosts := map[string]Host{
		"server1": {
			AddedAt:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			RemotePort:  22,
			RemoteUser: "admin",
			RemotePath: "/opt/app",
			Termius:    true,
		},
		"server2": {
			AddedAt:    time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
			RemotePort:  2222,
			RemoteUser: "deploy",
			RemotePath: "/home/deploy",
			Termius:    false,
		},
		"server3": {
			AddedAt:    time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
			RemotePort:  22,
			RemoteUser: "root",
			RemotePath: "/root",
			Termius:    false,
		},
	}

	for name, h := range hosts {
		cfg.AddHost(name, h)
	}
	if len(cfg.Hosts) != 3 {
		t.Errorf("After adding 3 hosts: len(Hosts) = %d, want 3", len(cfg.Hosts))
	}

	// Remove one host
	cfg.RemoveHost("server2")
	if len(cfg.Hosts) != 2 {
		t.Errorf("After RemoveHost: len(Hosts) = %d, want 2", len(cfg.Hosts))
	}
	if _, ok := cfg.Hosts["server2"]; ok {
		t.Error("server2 should have been removed")
	}

	// Remaining hosts should be intact
	if _, ok := cfg.Hosts["server1"]; !ok {
		t.Error("server1 should still exist")
	}
	if _, ok := cfg.Hosts["server3"]; !ok {
		t.Error("server3 should still exist")
	}
}

func TestSaveExistingDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := Default()
	cfg.Port = 4444
	cfg.AuditLog = "/tmp/test-audit.log"

	// First save creates the file
	if err := Save(cfg, path); err != nil {
		t.Fatalf("First Save failed: %v", err)
	}

	// Second save overwrites the existing file
	cfg.Port = 5555
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Second Save failed: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Port != 5555 {
		t.Errorf("Loaded Port = %d, want 5555", loaded.Port)
	}
}

func TestMergeDefaultsAllZero(t *testing.T) {
	cfg := &Config{}
	mergeDefaults(cfg)

	if cfg.Port != DefaultPort {
		t.Errorf("Port = %d, want %d", cfg.Port, DefaultPort)
	}
	if cfg.Transport != DefaultTransport {
		t.Errorf("Transport = %q, want %q", cfg.Transport, DefaultTransport)
	}
	if cfg.LogLevel != DefaultLogLevel {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, DefaultLogLevel)
	}
	if cfg.MaxImageBytes != DefaultMaxImageBytes {
		t.Errorf("MaxImageBytes = %d, want %d", cfg.MaxImageBytes, DefaultMaxImageBytes)
	}
	if cfg.MaxInFlight != DefaultMaxInFlight {
		t.Errorf("MaxInFlight = %d, want %d", cfg.MaxInFlight, DefaultMaxInFlight)
	}
	if cfg.RateLimitPerMinute != DefaultRateLimitPerMin {
		t.Errorf("RateLimitPerMinute = %d, want %d", cfg.RateLimitPerMinute, DefaultRateLimitPerMin)
	}
	if cfg.Hosts == nil {
		t.Error("Hosts should be initialized by mergeDefaults")
	}
}

func TestMergeDefaultsPartialOverride(t *testing.T) {
	cfg := &Config{
		Port:    8080,
		Hosts:   map[string]Host{"existing": {}},
	}

	mergeDefaults(cfg)

	// Overridden value should be preserved
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
	// Missing fields should get defaults
	if cfg.Transport != DefaultTransport {
		t.Errorf("Transport = %q, want %q", cfg.Transport, DefaultTransport)
	}
	if cfg.LogLevel != DefaultLogLevel {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, DefaultLogLevel)
	}
	if cfg.MaxImageBytes != DefaultMaxImageBytes {
		t.Errorf("MaxImageBytes = %d, want %d", cfg.MaxImageBytes, DefaultMaxImageBytes)
	}
	if cfg.MaxInFlight != DefaultMaxInFlight {
		t.Errorf("MaxInFlight = %d, want %d", cfg.MaxInFlight, DefaultMaxInFlight)
	}
	if cfg.RateLimitPerMinute != DefaultRateLimitPerMin {
		t.Errorf("RateLimitPerMinute = %d, want %d", cfg.RateLimitPerMinute, DefaultRateLimitPerMin)
	}
	// Existing hosts should be preserved
	if _, ok := cfg.Hosts["existing"]; !ok {
		t.Error("existing host should be preserved")
	}
}

func TestEnvOverrideInvalidPort(t *testing.T) {
	os.Setenv("CLIPBRIDGE_PORT", "not-a-number")
	defer os.Unsetenv("CLIPBRIDGE_PORT")

	cfg := Default()
	applyEnvOverrides(cfg)

	// Invalid port env should not change the default
	if cfg.Port != DefaultPort {
		t.Errorf("Port = %d, want default %d (invalid env ignored)", cfg.Port, DefaultPort)
	}
}

func TestEnvOverrideZeroPort(t *testing.T) {
	os.Setenv("CLIPBRIDGE_PORT", "0")
	defer os.Unsetenv("CLIPBRIDGE_PORT")

	cfg := Default()
	applyEnvOverrides(cfg)

	// Zero port should not override (port > 0 check)
	if cfg.Port != DefaultPort {
		t.Errorf("Port = %d, want default %d (zero port ignored)", cfg.Port, DefaultPort)
	}
}

func TestLoadWithHostsSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
port = 9999

[hosts.alpha]
remote_port = 22
remote_user = "user1"
remote_path = "/home/user1"
termius = true

[hosts.beta]
remote_port = 2222
remote_user = "user2"
remote_path = "/home/user2"
termius = false
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(cfg.Hosts) != 2 {
		t.Fatalf("len(Hosts) = %d, want 2", len(cfg.Hosts))
	}

	alpha, ok := cfg.Hosts["alpha"]
	if !ok {
		t.Fatal("Host 'alpha' not found")
	}
	if alpha.RemotePort != 22 {
		t.Errorf("alpha.RemotePort = %d, want 22", alpha.RemotePort)
	}
	if alpha.RemoteUser != "user1" {
		t.Errorf("alpha.RemoteUser = %q, want %q", alpha.RemoteUser, "user1")
	}
	if !alpha.Termius {
		t.Error("alpha.Termius = false, want true")
	}

	beta, ok := cfg.Hosts["beta"]
	if !ok {
		t.Fatal("Host 'beta' not found")
	}
	if beta.RemotePort != 2222 {
		t.Errorf("beta.RemotePort = %d, want 2222", beta.RemotePort)
	}
	if beta.Termius {
		t.Error("beta.Termius = true, want false")
	}
}

func TestSaveAndReloadWithHosts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := Default()
	cfg.Port = 8080
	cfg.AddHost("host1", Host{
		AddedAt:    time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		RemotePort:  22,
		RemoteUser: "admin",
		RemotePath: "/opt",
		Termius:    true,
	})
	cfg.AddHost("host2", Host{
		AddedAt:    time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
		RemotePort:  2222,
		RemoteUser: "deploy",
		RemotePath: "/home/deploy",
		Termius:    false,
	})

	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(loaded.Hosts) != 2 {
		t.Errorf("len(Hosts) = %d, want 2", len(loaded.Hosts))
	}

	h1, ok := loaded.Hosts["host1"]
	if !ok {
		t.Fatal("host1 not found")
	}
	if h1.RemoteUser != "admin" {
		t.Errorf("host1.RemoteUser = %q, want %q", h1.RemoteUser, "admin")
	}
	if !h1.Termius {
		t.Error("host1.Termius = false, want true")
	}
}
