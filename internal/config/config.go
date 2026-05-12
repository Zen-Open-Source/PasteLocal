package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	DefaultPort            = 7331
	DefaultTransport       = "tcp"
	DefaultLogLevel        = "info"
	DefaultMaxImageBytes   = 52428800 // 50 MiB
	DefaultMaxInFlight     = 4
	DefaultRateLimitPerMin = 60
	DefaultConfigDir       = "~/.config/pastelocal"
	DefaultConfigFile      = "config.toml"
)

type Config struct {
	Port               int             `toml:"port"`
	Transport          string          `toml:"transport"`
	LogLevel           string          `toml:"log_level"`
	MaxImageBytes      int64           `toml:"max_image_bytes"`
	MaxInFlight        int             `toml:"max_in_flight"`
	RateLimitPerMinute int             `toml:"rate_limit_per_minute"`
	AuditLog           string          `toml:"audit_log"`
	MacOS              MacOSConfig     `toml:"macos"`
	Linux              LinuxConfig     `toml:"linux"`
	Hosts              map[string]Host `toml:"hosts"`
}

type MacOSConfig struct {
	PngpastePath string `toml:"pngpaste_path"`
}

type LinuxConfig struct {
	ClipboardTool string `toml:"clipboard_tool"` // "auto" | "wl-paste" | "xclip"
}

type Host struct {
	AddedAt    time.Time `toml:"added_at"`
	RemotePort int       `toml:"remote_port"`
	RemoteUser string    `toml:"remote_user"`
	RemotePath string    `toml:"remote_path"`
	Termius    bool      `toml:"termius"`
}

// mu protects file operations during Save to prevent concurrent writes.
var mu sync.Mutex

// Default returns a Config populated with default values.
func Default() *Config {
	return &Config{
		Port:               DefaultPort,
		Transport:          DefaultTransport,
		LogLevel:           DefaultLogLevel,
		MaxImageBytes:      DefaultMaxImageBytes,
		MaxInFlight:        DefaultMaxInFlight,
		RateLimitPerMinute: DefaultRateLimitPerMin,
		Hosts:              make(map[string]Host),
	}
}

// ConfigDir returns the expanded configuration directory path.
// It checks the PASTELOCAL_CONFIG_DIR environment variable first,
// then falls back to ~/.config/pastelocal/.
func ConfigDir() string {
	if dir := os.Getenv("PASTELOCAL_CONFIG_DIR"); dir != "" {
		return expandHome(dir)
	}
	return expandHome(DefaultConfigDir)
}

// ConfigPath returns the full path to the configuration file.
func ConfigPath() string {
	return filepath.Join(ConfigDir(), DefaultConfigFile)
}

// Load reads configuration from the given path.
// If the file does not exist, it returns Default().
// Missing fields in the TOML file are filled with default values.
// Environment variables override the loaded values.
func Load(path string) (*Config, error) {
	cfg := Default()

	path = expandHome(path)

	if _, err := os.Stat(path); os.IsNotExist(err) {
		applyEnvOverrides(cfg)
		return cfg, nil
	}

	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, fmt.Errorf("config: failed to parse %s: %w", path, err)
	}

	mergeDefaults(cfg)
	applyEnvOverrides(cfg)

	return cfg, nil
}

// Save writes the configuration to the given path atomically.
// It writes to a temporary file in the same directory, fsyncs,
// then renames the temp file to the target path.
// It creates the directory if it does not exist.
func Save(c *Config, path string) error {
	mu.Lock()
	defer mu.Unlock()

	path = expandHome(path)
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("config: failed to create directory %s: %w", dir, err)
	}

	// Write to temp file in the same directory to ensure atomic rename.
	tmp, err := os.CreateTemp(dir, "pastelocal-config-*")
	if err != nil {
		return fmt.Errorf("config: failed to create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if err := toml.NewEncoder(tmp).Encode(c); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("config: failed to encode config: %w", err)
	}

	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("config: failed to sync temp file: %w", err)
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("config: failed to close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("config: failed to rename temp file: %w", err)
	}

	return nil
}

// AddHost adds a host entry to the configuration.
func (c *Config) AddHost(name string, h Host) {
	if c.Hosts == nil {
		c.Hosts = make(map[string]Host)
	}
	c.Hosts[name] = h
}

// RemoveHost removes a host entry from the configuration.
func (c *Config) RemoveHost(name string) {
	delete(c.Hosts, name)
}

// mergeDefaults fills in zero-valued fields with defaults.
func mergeDefaults(cfg *Config) {
	if cfg.Port == 0 {
		cfg.Port = DefaultPort
	}
	if cfg.Transport == "" {
		cfg.Transport = DefaultTransport
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = DefaultLogLevel
	}
	if cfg.MaxImageBytes == 0 {
		cfg.MaxImageBytes = DefaultMaxImageBytes
	}
	if cfg.MaxInFlight == 0 {
		cfg.MaxInFlight = DefaultMaxInFlight
	}
	if cfg.RateLimitPerMinute == 0 {
		cfg.RateLimitPerMinute = DefaultRateLimitPerMin
	}
	if cfg.Hosts == nil {
		cfg.Hosts = make(map[string]Host)
	}
}

// applyEnvOverrides applies environment variable overrides to the config.
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("PASTELOCAL_PORT"); v != "" {
		var port int
		if _, err := fmt.Sscanf(v, "%d", &port); err == nil && port > 0 {
			cfg.Port = port
		}
	}
	if v := os.Getenv("PASTELOCAL_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			home = os.Getenv("HOME")
		}
		return filepath.Join(home, path[1:])
	}
	return path
}
