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
	DefaultMaxTextBytes    = 1048576  // 1 MiB
	DefaultMaxInFlight     = 4
	DefaultRateLimitPerMin = 60
	DefaultConfigDir       = "~/.config/pastelocal"
	DefaultConfigFile      = "config.toml"
	DefaultHistorySize     = 10
	DefaultHistoryTTL      = 3600 // seconds (1 hour)
)

type Config struct {
	Port               int             `toml:"port"`
	Transport          string          `toml:"transport"`
	LogLevel           string          `toml:"log_level"`
	MaxImageBytes      int64           `toml:"max_image_bytes"`
	MaxTextBytes       int64           `toml:"max_text_bytes"`
	MaxInFlight        int             `toml:"max_in_flight"`
	RateLimitPerMinute int             `toml:"rate_limit_per_minute"`
	AuditLog           string          `toml:"audit_log"`
	AllowedFormats     []string        `toml:"allowed_formats"`
	MacOS              MacOSConfig     `toml:"macos"`
	Linux              LinuxConfig     `toml:"linux"`
	Hosts              map[string]Host `toml:"hosts"`
	History            HistoryConfig   `toml:"history"`
	Redaction          RedactionConfig `toml:"redaction"`
	Processors         ProcessorConfig `toml:"processors"`
	Relay              RelayConfig     `toml:"relay"`
}

type MacOSConfig struct {
	PngpastePath string `toml:"pngpaste_path"`
}

type LinuxConfig struct {
	ClipboardTool string `toml:"clipboard_tool"` // "auto" | "wl-paste" | "xclip"
}

type Host struct {
	AddedAt     time.Time   `toml:"added_at"`
	RemotePort  int         `toml:"remote_port"`
	RemoteUser  string      `toml:"remote_user"`
	RemotePath  string      `toml:"remote_path"`
	Termius     bool        `toml:"termius"`
	Permissions []string    `toml:"permissions"` // "read", "write"
	TokenHash   string      `toml:"token_hash"`  // SHA-256 of per-host token
}

// HistoryConfig controls the clipboard history ring buffer.
type HistoryConfig struct {
	Enabled bool `toml:"enabled"`
	Size    int  `toml:"size"`
	TTL     int  `toml:"ttl_seconds"`
}

// RedactionConfig controls content filtering and redaction rules.
type RedactionConfig struct {
	Enabled bool            `toml:"enabled"`
	Rules   []RedactionRule `toml:"rules"`
}

// RedactionRule defines a single content redaction rule.
type RedactionRule struct {
	Name        string `toml:"name"`
	Pattern     string `toml:"pattern"`      // regex pattern
	Action      string `toml:"action"`        // "redact" or "block"
	Description string `toml:"description"`
}

// ProcessorConfig controls the clipboard processor pipeline.
type ProcessorConfig struct {
	Enabled    bool              `toml:"enabled"`
	Timeout    int               `toml:"timeout_seconds"`
	Chain      []ProcessorEntry  `toml:"chain"`
}

// ProcessorEntry represents a single processor in the pipeline.
type ProcessorEntry struct {
	Name    string `toml:"name"`
	Command string `toml:"command"`
}

// RelayConfig controls the E2E encrypted relay for multi-device sync.
type RelayConfig struct {
	Enabled      bool   `toml:"enabled"`
	RelayURL     string `toml:"relay_url"`
	DeviceID     string `toml:"device_id"`
	DeviceKeyPath string `toml:"device_key_path"`
	AuthTokenPath string `toml:"auth_token_path"`
	AutoUpload   bool   `toml:"auto_upload"`
	UploadTTL    int    `toml:"upload_ttl"` // seconds
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
		MaxTextBytes:       DefaultMaxTextBytes,
		MaxInFlight:        DefaultMaxInFlight,
		RateLimitPerMinute: DefaultRateLimitPerMin,
		AllowedFormats:     []string{"png", "text"},
		Hosts:              make(map[string]Host),
		History: HistoryConfig{
			Enabled: true,
			Size:    DefaultHistorySize,
			TTL:     DefaultHistoryTTL,
		},
		Redaction: RedactionConfig{
			Enabled: true,
			Rules:   DefaultRedactionRules(),
		},
		Processors: ProcessorConfig{
			Enabled: false,
			Timeout: 5,
		},
		Relay: RelayConfig{
			Enabled:      false,
			RelayURL:     "http://localhost:7332",
			DeviceID:     "",
			DeviceKeyPath: "~/.config/pastelocal/device-key",
			AuthTokenPath: "~/.config/pastelocal/relay-token",
			AutoUpload:   false,
			UploadTTL:    300,
		},
	}
}

// DefaultRedactionRules returns built-in redaction rules for common secrets.
func DefaultRedactionRules() []RedactionRule {
	return []RedactionRule{
		{
			Name:        "aws-access-key",
			Pattern:     `AKIA[0-9A-Z]{16}`,
			Action:      "block",
			Description: "AWS access key ID",
		},
		{
			Name:        "aws-secret-key",
			Pattern:     `(?i)aws(.{0,20})?(?-i)['\"][0-9a-zA-Z/+]{40}['\"]`,
			Action:      "block",
			Description: "AWS secret access key",
		},
		{
			Name:        "github-token",
			Pattern:     `gh[porsu]_[0-9a-zA-Z]{36}`,
			Action:      "block",
			Description: "GitHub personal access token",
		},
		{
			Name:        "private-key",
			Pattern:     `-----BEGIN (?:RSA |EC |DSA )?PRIVATE KEY-----`,
			Action:      "block",
			Description: "PEM private key header",
		},
		{
			Name:        "credit-card",
			Pattern:     `\b4[0-9]{12}(?:[0-9]{3})?\b|\b5[1-5][0-9]{14}\b|\b3[47][0-9]{13}\b`,
			Action:      "block",
			Description: "Credit card number (Visa/MC/Amex)",
		},
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

// IsFormatAllowed returns true if the given format is in the allowed list.
func (c *Config) IsFormatAllowed(format string) bool {
	for _, f := range c.AllowedFormats {
		if f == format {
			return true
		}
	}
	return false
}

// HostHasPermission returns true if the given host has the specified permission.
// If the host has no permissions set, it defaults to having all permissions.
func (c *Config) HostHasPermission(alias, permission string) bool {
	h, ok := c.Hosts[alias]
	if !ok {
		return false
	}
	if len(h.Permissions) == 0 {
		return true // default: all permissions
	}
	for _, p := range h.Permissions {
		if p == permission || p == "read+write" {
			return true
		}
	}
	return false
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
	if cfg.MaxTextBytes == 0 {
		cfg.MaxTextBytes = DefaultMaxTextBytes
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
	if len(cfg.AllowedFormats) == 0 {
		cfg.AllowedFormats = []string{"png", "text"}
	}
	if cfg.History.Size == 0 {
		cfg.History.Size = DefaultHistorySize
	}
	if cfg.History.TTL == 0 {
		cfg.History.TTL = DefaultHistoryTTL
	}
	if cfg.Processors.Timeout == 0 {
		cfg.Processors.Timeout = 5
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
