package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/pastelocal/pastelocal/internal/auth"
	"github.com/pastelocal/pastelocal/internal/config"
	"github.com/pastelocal/pastelocal/internal/crypto"
	"github.com/pastelocal/pastelocal/internal/doctor"
	"github.com/pastelocal/pastelocal/internal/hostinstall"
	"github.com/pastelocal/pastelocal/internal/proto"
	"github.com/pastelocal/pastelocal/internal/relay"
	"github.com/pastelocal/pastelocal/internal/service"
	"github.com/pastelocal/pastelocal/internal/tui"
)

// version is set via ldflags at build time.
var version = "dev"

var (
	cfgPath string
	verbose bool
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// rootCmd is the base command for pastelocal CLI.
var rootCmd = &cobra.Command{
	Use:     "pastelocal",
	Short:   "Pastelocal — secure local-to-remote clipboard bridge",
	Version: version,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgPath, "config", config.ConfigPath(), "config file path")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	rootCmd.AddGroup(&cobra.Group{ID: "daemon", Title: "Daemon Management"})
	rootCmd.AddGroup(&cobra.Group{ID: "host", Title: "Host Management"})
	rootCmd.AddGroup(&cobra.Group{ID: "diagnostic", Title: "Diagnostics"})
	rootCmd.AddGroup(&cobra.Group{ID: "grok", Title: "Grok Integration"})
}

// fail prints a single-line error to stderr and returns an error for cobra.
func fail(format string, args ...interface{}) error {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(os.Stderr, msg)
	return fmt.Errorf("%s", msg)
}

// loadConfig loads the config from the --config flag path.
func loadConfig() (*config.Config, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	return cfg, nil
}

// saveConfig saves the config to the --config flag path.
func saveConfig(cfg *config.Config) error {
	return config.Save(cfg, cfgPath)
}

// newLogger creates an slog logger, respecting the verbose flag.
func newLogger() *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

// daemonBinaryPath returns the path to the pastelocald binary,
// searching in the same directory as the current executable.
func daemonBinaryPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "pastelocald"
	}
	dir := filepath.Dir(exe)
	candidate := filepath.Join(dir, "pastelocald")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return "pastelocald"
}

// findSkillPath searches for the paste.md skill file.
func findSkillPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	candidates := []string{
		filepath.Join(home, ".config", "pastelocal", "skill", "paste.md"),
		filepath.Join(home, ".local", "share", "pastelocal", "skill", "paste.md"),
	}
	exe, err := os.Executable()
	if err == nil {
		candidates = append(candidates,
			filepath.Join(filepath.Dir(exe), "..", "skill", "paste.md"),
		)
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// findLocalBinaryDir returns the directory containing the
// pastelocal-remote-* binaries.
func findLocalBinaryDir() string {
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		if _, err := os.Stat(filepath.Join(dir, "pastelocal-remote-amd64")); err == nil {
			return dir
		}
	}
	home, err := os.UserHomeDir()
	if err == nil {
		dir := filepath.Join(home, ".local", "share", "pastelocal")
		if _, err := os.Stat(filepath.Join(dir, "pastelocal-remote-amd64")); err == nil {
			return dir
		}
	}
	return ""
}

// newService creates a Service for the loaded config.
func newService(cfg *config.Config) *service.Service {
	return service.New(daemonBinaryPath(), cfgPath, cfg.Port)
}

// ── init ──────────────────────────────────────────────────────────────────────

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize pastelocal configuration and start the daemon",
	GroupID: "daemon",
	RunE:  runInit,
}

var initPort int
var initNoKeychain bool

func init() {
	initCmd.Flags().IntVar(&initPort, "port", 0, "listen port (default 7331)")
	initCmd.Flags().BoolVar(&initNoKeychain, "no-keychain", false, "skip keychain, use file-only token storage")
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	// 1. Generate token.
	token, err := auth.GenerateToken()
	if err != nil {
		return fail("failed to generate token: %v", err)
	}

	// 2. Write config.toml with defaults (and port override).
	cfg := config.Default()
	if initPort != 0 {
		cfg.Port = initPort
	}
	// Make sure the config directory exists.
	cfgDir := config.ConfigDir()
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		return fail("failed to create config directory: %v", err)
	}
	if err := saveConfig(cfg); err != nil {
		return fail("failed to write config: %v", err)
	}

	// 3. Store token (keychain if available, file fallback; --no-keychain skips keychain).
	useKeychain := !initNoKeychain
	store := auth.NewTokenStore(useKeychain, auth.DefaultTokenPath())
	if err := store.Store(token); err != nil {
		return fail("failed to store token: %v", err)
	}

	// Determine token location for display.
	tokenLocation := "file"
	if useKeychain {
		tokenLocation = "keychain (fallback: file)"
	}

	// 4. Install service unit.
	svc := newService(cfg)
	if err := svc.Install(); err != nil {
		return fail("failed to install service: %v", err)
	}

	// 5. Start daemon.
	if err := svc.Start(); err != nil {
		return fail("failed to start daemon: %v", err)
	}

	// 6. Get PID.
	_, pid, _ := svc.Status()

	// Print success message.
	fmt.Printf("pastelocal initialized successfully!\n")
	fmt.Printf("  port: %d\n", cfg.Port)
	fmt.Printf("  token location: %s\n", tokenLocation)
	fmt.Printf("  daemon PID: %d\n", pid)
	fmt.Printf("  config: %s\n", cfgPath)
	return nil
}

// ── add-host ──────────────────────────────────────────────────────────────────

var addHostCmd = &cobra.Command{
	Use:   "add-host <alias>",
	Short: "Add a remote host for clipboard sharing",
	GroupID: "host",
	Args:  cobra.ExactArgs(1),
	RunE:  runAddHost,
}

var (
	addHostTermius         bool
	addHostFinish          bool
	addHostReinstall       bool
	addHostUpdateTokenOnly bool
	addHostSeparateToken   bool
)

func init() {
	addHostCmd.Flags().BoolVar(&addHostTermius, "termius", false, "print Termius port-forwarding instructions")
	addHostCmd.Flags().BoolVar(&addHostFinish, "finish", false, "finish installation (skip ssh config edit)")
	addHostCmd.Flags().BoolVar(&addHostReinstall, "reinstall", false, "reinstall even if already installed")
	addHostCmd.Flags().BoolVar(&addHostUpdateTokenOnly, "update-token-only", false, "only scp the new token")
	addHostCmd.Flags().BoolVar(&addHostSeparateToken, "separate-token", false, "generate per-host token")
	rootCmd.AddCommand(addHostCmd)
}

func runAddHost(cmd *cobra.Command, args []string) error {
	alias := args[0]

	// 1. Load config.
	cfg, err := loadConfig()
	if err != nil {
		return fail("%v", err)
	}

	// 2. If --termius: call PrintTermiusInstructions, then return.
	if addHostTermius {
		port := cfg.Port
		if err := hostinstall.PrintTermiusInstructions(alias, port); err != nil {
			return fail("failed to print Termius instructions: %v", err)
		}
		return nil
	}

	// 3. Build installer options.
	opts := hostinstall.Options{
		Alias:           alias,
		Host:            alias,
		Port:            cfg.Port,
		Finish:          addHostFinish,
		Reinstall:       addHostReinstall,
		UpdateTokenOnly: addHostUpdateTokenOnly,
		SeparateToken:   addHostSeparateToken,
		TokenPath:       auth.DefaultTokenPath(),
		SkillPath:       findSkillPath(),
		LocalBinaryDir:  findLocalBinaryDir(),
	}

	// Resolve user from existing host config if present.
	if h, ok := cfg.Hosts[alias]; ok {
		opts.User = h.RemoteUser
		opts.RemotePath = h.RemotePath
		if h.RemotePort != 0 {
			opts.Port = h.RemotePort
		}
	}

	logger := newLogger()
	installer := hostinstall.NewInstaller(opts, logger)

	// 4. Call Install().
	if err := installer.Install(); err != nil {
		return fail("failed to install host %s: %v", alias, err)
	}

	// 5. Update config with host entry.
	hostEntry := config.Host{
		AddedAt:    time.Now().UTC(),
		RemotePort: opts.Port,
		RemoteUser: opts.User,
		RemotePath: opts.RemotePath,
		Termius:    addHostTermius,
	}
	cfg.AddHost(alias, hostEntry)
	if err := saveConfig(cfg); err != nil {
		return fail("failed to save config: %v", err)
	}

	// 6. Print success.
	fmt.Printf("Host %s added successfully.\n", alias)
	return nil
}

// ── remove-host ───────────────────────────────────────────────────────────────

var removeHostCmd = &cobra.Command{
	Use:   "remove-host <alias>",
	Short: "Remove a remote host from pastelocal",
	GroupID: "host",
	Args:  cobra.ExactArgs(1),
	RunE:  runRemoveHost,
}

func init() {
	rootCmd.AddCommand(removeHostCmd)
}

func runRemoveHost(cmd *cobra.Command, args []string) error {
	alias := args[0]

	// 1. Load config.
	cfg, err := loadConfig()
	if err != nil {
		return fail("%v", err)
	}

	// 2. Find host in config.
	hostCfg, ok := cfg.Hosts[alias]
	if !ok {
		return fail("host %q not found in config", alias)
	}

	// 3. Call installer.Uninstall().
	opts := hostinstall.Options{
		Alias:      alias,
		Host:       alias,
		User:       hostCfg.RemoteUser,
		Port:       hostCfg.RemotePort,
		RemotePath: hostCfg.RemotePath,
		Termius:    hostCfg.Termius,
	}
	logger := newLogger()
	installer := hostinstall.NewInstaller(opts, logger)
	if err := installer.Uninstall(); err != nil {
		return fail("failed to uninstall host %s: %v", alias, err)
	}

	// 4. Remove host from config.
	cfg.RemoveHost(alias)

	// 5. Save config.
	if err := saveConfig(cfg); err != nil {
		return fail("failed to save config: %v", err)
	}

	// 6. Print success.
	fmt.Printf("Host %s removed successfully.\n", alias)
	return nil
}

// ── list-hosts ────────────────────────────────────────────────────────────────

var listHostsCmd = &cobra.Command{
	Use:   "list-hosts",
	Short: "List configured remote hosts",
	GroupID: "host",
	RunE:  runListHosts,
}

var listHostsJSON bool

func init() {
	listHostsCmd.Flags().BoolVar(&listHostsJSON, "json", false, "output as JSON")
	rootCmd.AddCommand(listHostsCmd)
}

type hostEntry struct {
	Alias      string `json:"alias"`
	AddedAt    string `json:"added_at"`
	RemotePort int    `json:"remote_port"`
	User       string `json:"user"`
	Termius    bool   `json:"termius"`
}

func runListHosts(cmd *cobra.Command, args []string) error {
	// 1. Load config.
	cfg, err := loadConfig()
	if err != nil {
		return fail("%v", err)
	}

	if listHostsJSON {
		// 3. --json: print as JSON array.
		var entries []hostEntry
		for alias, h := range cfg.Hosts {
			entries = append(entries, hostEntry{
				Alias:      alias,
				AddedAt:    h.AddedAt.Format(time.RFC3339),
				RemotePort: h.RemotePort,
				User:       h.RemoteUser,
				Termius:    h.Termius,
			})
		}
		if entries == nil {
			entries = []hostEntry{}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(entries); err != nil {
			return fail("failed to encode JSON: %v", err)
		}
		return nil
	}

	// 2. Print table of hosts.
	if len(cfg.Hosts) == 0 {
		fmt.Println("No hosts configured. Run `pastelocal add-host <alias>` to add one.")
		return nil
	}

	fmt.Printf("%-20s %-22s %-11s %-15s %s\n", "ALIAS", "ADDED_AT", "REMOTE_PORT", "USER", "TERMIUS")
	for alias, h := range cfg.Hosts {
		termiusFlag := ""
		if h.Termius {
			termiusFlag = "yes"
		}
		fmt.Printf("%-20s %-22s %-11d %-15s %s\n",
			alias,
			h.AddedAt.Format(time.DateOnly),
			h.RemotePort,
			h.RemoteUser,
			termiusFlag,
		)
	}
	return nil
}

// ── status ────────────────────────────────────────────────────────────────────

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show pastelocal daemon status",
	GroupID: "diagnostic",
	RunE:  runStatus,
}

var statusJSON bool

func init() {
	statusCmd.Flags().BoolVar(&statusJSON, "json", false, "output as JSON")
	rootCmd.AddCommand(statusCmd)
}

type statusOutput struct {
	Version    string            `json:"version"`
	Running    bool              `json:"running"`
	PID        int               `json:"pid,omitempty"`
	Port       int               `json:"port"`
	Transport  string            `json:"transport"`
	Hosts      map[string]string `json:"hosts"`
	LastRead   string            `json:"last_read,omitempty"`
	Uptime     string            `json:"uptime,omitempty"`
}

func runStatus(cmd *cobra.Command, args []string) error {
	// 1. Load config.
	cfg, err := loadConfig()
	if err != nil {
		return fail("%v", err)
	}

	svc := newService(cfg)
	running, pid, _ := svc.Status()

	// 2. Check daemon health via HTTP.
	daemonHealthy := false
	if running {
		url := fmt.Sprintf("http://127.0.0.1:%d/health", cfg.Port)
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			daemonHealthy = resp.StatusCode == http.StatusOK
		}
	}

	// 3. Build host status map.
	hostStatuses := make(map[string]string)
	for alias, h := range cfg.Hosts {
		status := "ok"
		if !daemonHealthy {
			status = "unreachable"
		} else if h.Termius {
			status = "ok (termius)"
		}
		hostStatuses[alias] = status
	}

	// 4. Compute uptime.
	uptimeStr := ""
	if running && pid > 0 {
		uptimeStr = formatUptime(pid)
	}

	// 5. Format output.
	stateStr := "stopped"
	if running {
		if daemonHealthy {
			stateStr = "running"
		} else {
			stateStr = "degraded"
		}
	}

	if statusJSON {
		out := statusOutput{
			Version:   version,
			Running:   running,
			PID:       pid,
			Port:      cfg.Port,
			Transport: cfg.Transport,
			Hosts:     hostStatuses,
			Uptime:    uptimeStr,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			return fail("failed to encode JSON: %v", err)
		}
		return nil
	}

	// Human-readable output (spec §11.4 style).
	fmt.Printf("pastelocal %s — %s\n", version, stateStr)
	fmt.Printf("  port: %d (%s, loopback)\n", cfg.Port, cfg.Transport)
	if len(hostStatuses) > 0 {
		var parts []string
		for alias, s := range hostStatuses {
			parts = append(parts, fmt.Sprintf("%s (%s)", alias, s))
		}
		fmt.Printf("  hosts: %s\n", strings.Join(parts, ", "))
	} else {
		fmt.Println("  hosts: (none)")
	}
	if uptimeStr != "" {
		fmt.Printf("  uptime: %s\n", uptimeStr)
	}
	return nil
}

// formatUptime derives the uptime from the process start time.
func formatUptime(pid int) string {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("ps", "-o", "etime=", "-p", fmt.Sprintf("%d", pid))
	case "linux":
		cmd = exec.Command("ps", "-o", "etime=", "-p", fmt.Sprintf("%d", pid))
	default:
		return ""
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ── doctor ────────────────────────────────────────────────────────────────────

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run diagnostic checks",
	GroupID: "diagnostic",
	RunE:  runDoctor,
}

var (
	doctorFix  bool
	doctorHost string
)

func init() {
	doctorCmd.Flags().BoolVar(&doctorFix, "fix", false, "auto-fix issues where safe")
	doctorCmd.Flags().StringVar(&doctorHost, "host", "", "run checks for a specific host")
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, args []string) error {
	// 1. Load config.
	cfg, err := loadConfig()
	if err != nil {
		return fail("%v", err)
	}

	// 2. Run checks.
	var results []doctor.CheckResult
	results = append(results, doctor.RunChecks(cfg, doctorFix)...)

	// 3. If --host, also run RunHostChecks.
	if doctorHost != "" {
		results = append(results, doctor.RunHostChecks(cfg, doctorHost, doctorFix)...)
	}

	// 4. Print results.
	doctor.PrintResults(os.Stdout, results)

	// 5. Print summary.
	failed := 0
	for _, r := range results {
		if !r.Passed {
			failed++
		}
	}

	fmt.Println()
	if failed > 0 {
		fmt.Printf("%d issue(s) found. Run `pastelocal doctor --fix` to auto-fix where safe.\n", failed)
	} else {
		fmt.Println("All checks passed.")
	}

	// 6. Exit non-zero if any checks failed.
	if failed > 0 {
		return fail("%d issue(s) found", failed)
	}
	return nil
}

// ── logs ──────────────────────────────────────────────────────────────────────

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Show pastelocal daemon logs",
	GroupID: "diagnostic",
	RunE:  runLogs,
}

var (
	logsFollow bool
	logsLines  int
)

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "follow log output")
	logsCmd.Flags().IntVar(&logsLines, "lines", 20, "show last N lines")
	rootCmd.AddCommand(logsCmd)
}

func runLogs(cmd *cobra.Command, args []string) error {
	switch runtime.GOOS {
	case "darwin":
		return runLogsDarwin()
	case "linux":
		return runLogsLinux()
	default:
		return fail("logs not supported on this platform")
	}
}

func runLogsDarwin() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fail("cannot determine home directory: %v", err)
	}
	logFile := filepath.Join(home, "Library", "Logs", "pastelocal.log")
	return tailLogFile(logFile)
}

func runLogsLinux() error {
	// Use journalctl for systemd-based systems.
	if logsFollow {
		cmd := exec.Command("journalctl", "--user", "-u", "pastelocal.service", "-f", "-n", fmt.Sprintf("%d", logsLines))
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fail("journalctl failed: %v", err)
		}
		return nil
	}
	cmd := exec.Command("journalctl", "--user", "-u", "pastelocal.service", "-n", fmt.Sprintf("%d", logsLines), "--no-pager")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fail("journalctl failed: %v", err)
	}
	return nil
}

func tailLogFile(path string) error {
	if _, err := os.Stat(path); err != nil {
		return fail("log file not found: %s", path)
	}

	if logsFollow {
		cmd := exec.Command("tail", "-f", "-n", fmt.Sprintf("%d", logsLines), path)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fail("tail failed: %v", err)
		}
		return nil
	}

	f, err := os.Open(path)
	if err != nil {
		return fail("cannot open log file: %v", err)
	}
	defer f.Close()

	// Seek to the right position for the last N lines.
	// Simple approach: use tail command.
	cmd := exec.Command("tail", "-n", fmt.Sprintf("%d", logsLines), path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fail("tail failed: %v", err)
	}
	return nil
}

// ── start / stop / restart ────────────────────────────────────────────────────

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the pastelocal daemon",
	GroupID: "daemon",
	RunE:  runStart,
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the pastelocal daemon",
	GroupID: "daemon",
	RunE:  runStop,
}

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the pastelocal daemon",
	GroupID: "daemon",
	RunE:  runRestart,
}

func init() {
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(restartCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return fail("%v", err)
	}
	svc := newService(cfg)
	if err := svc.Start(); err != nil {
		return fail("failed to start daemon: %v", err)
	}
	fmt.Println("pastelocal daemon started.")
	return nil
}

func runStop(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return fail("%v", err)
	}
	svc := newService(cfg)
	if err := svc.Stop(); err != nil {
		return fail("failed to stop daemon: %v", err)
	}
	fmt.Println("pastelocal daemon stopped.")
	return nil
}

func runRestart(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return fail("%v", err)
	}
	svc := newService(cfg)
	if err := svc.Restart(); err != nil {
		return fail("failed to restart daemon: %v", err)
	}
	fmt.Println("pastelocal daemon restarted.")
	return nil
}

// ── rotate-token ──────────────────────────────────────────────────────────────

var rotateTokenCmd = &cobra.Command{
	Use:   "rotate-token",
	Short: "Generate a new auth token and update local storage",
	GroupID: "daemon",
	RunE:  runRotateToken,
}

func init() {
	rootCmd.AddCommand(rotateTokenCmd)
}

func runRotateToken(cmd *cobra.Command, args []string) error {
	// 1. Generate new token.
	newToken, err := auth.GenerateToken()
	if err != nil {
		return fail("failed to generate token: %v", err)
	}

	// 2. Store new token (keychain + file).
	store := auth.NewTokenStore(true, auth.DefaultTokenPath())
	if err := store.Store(newToken); err != nil {
		return fail("failed to store new token: %v", err)
	}

	// 3. Load config.
	cfg, err := loadConfig()
	if err != nil {
		return fail("%v", err)
	}

	// 4. Print which hosts need token update.
	fmt.Println("Generated new token. Update these hosts:")
	if len(cfg.Hosts) == 0 {
		fmt.Println("  (no hosts configured)")
		return nil
	}
	for alias := range cfg.Hosts {
		fmt.Printf("  - %s\n", alias)
	}
	fmt.Println("Run:")
	for alias := range cfg.Hosts {
		fmt.Printf("  pastelocal add-host %s --update-token-only\n", alias)
	}
	return nil
}

// ── uninstall ─────────────────────────────────────────────────────────────────

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall pastelocal completely",
	GroupID: "daemon",
	RunE:  runUninstall,
}

var uninstallKeepConfig bool

func init() {
	uninstallCmd.Flags().BoolVar(&uninstallKeepConfig, "keep-config", false, "keep configuration files")
	rootCmd.AddCommand(uninstallCmd)
}

func runUninstall(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		// Config might already be gone; proceed with defaults.
		cfg = config.Default()
	}

	// 1. Stop daemon.
	svc := newService(cfg)
	_ = svc.Stop()

	// 2. Uninstall service unit.
	_ = svc.Uninstall()

	// 3. Delete token (keychain + file).
	store := auth.NewTokenStore(true, auth.DefaultTokenPath())
	_ = store.Delete()

	// 4. Remove config directory (unless --keep-config).
	if !uninstallKeepConfig {
		cfgDir := config.ConfigDir()
		if err := os.RemoveAll(cfgDir); err != nil {
			return fail("failed to remove config directory: %v", err)
		}
	}

	// 5. Print success.
	fmt.Println("pastelocal uninstalled successfully.")
	return nil
}

// ── dashboard ─────────────────────────────────────────────────────────────────

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Interactive TUI dashboard for monitoring pastelocal",
	GroupID: "diagnostic",
	RunE:  runDashboard,
}

func init() {
	rootCmd.AddCommand(dashboardCmd)
}

func runDashboard(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return fail("%v", err)
	}

	model := tui.NewModel(cfg, cfgPath)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return fail("dashboard error: %v", err)
	}
	return nil
}

// ── tokens ─────────────────────────────────────────────────────────────────────

var tokensCmd = &cobra.Command{
	Use:   "tokens",
	Short: "List and manage auth tokens",
	GroupID: "host",
	RunE:  runTokens,
}

var tokensRevoke string

func init() {
	tokensCmd.Flags().StringVar(&tokensRevoke, "revoke", "", "revoke token for host alias")
	rootCmd.AddCommand(tokensCmd)
}

func runTokens(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return fail("%v", err)
	}

	if tokensRevoke != "" {
		hostCfg, ok := cfg.Hosts[tokensRevoke]
		if !ok {
			return fail("host %q not found in config", tokensRevoke)
		}
		// Remove all permissions from the host.
		hostCfg.Permissions = []string{}
		cfg.Hosts[tokensRevoke] = hostCfg
		if err := saveConfig(cfg); err != nil {
			return fail("failed to save config: %v", err)
		}
		fmt.Printf("Revoked all permissions for host %s.\n", tokensRevoke)
		return nil
	}

	// List all hosts and their permissions.
	if len(cfg.Hosts) == 0 {
		fmt.Println("No hosts configured.")
		return nil
	}

	fmt.Printf("%-20s %-20s %s\n", "ALIAS", "PERMISSIONS", "TOKEN_HASH")
	for alias, h := range cfg.Hosts {
		perms := strings.Join(h.Permissions, ", ")
		if perms == "" {
			perms = "read+write (default)"
		}
		tokenHash := h.TokenHash
		if tokenHash == "" {
			tokenHash = "(shared token)"
		}
		fmt.Printf("%-20s %-20s %s\n", alias, perms, tokenHash)
	}
	return nil
}

// ── snippets ──────────────────────────────────────────────────────────────────

var snippetsCmd = &cobra.Command{
	Use:   "snippets",
	Short: "Manage clipboard snippets",
	GroupID: "daemon",
}

var (
	snippetSaveName        string
	snippetSaveFormat      string
	snippetSaveDescription string
	snippetSaveData        string
	snippetRemoveName      string
)

var snippetsSaveCmd = &cobra.Command{
	Use:   "save <name>",
	Short: "Save current clipboard as a named snippet",
	Args:  cobra.ExactArgs(1),
	RunE:  runSnippetSave,
}

var snippetsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all saved snippets",
	RunE:  runSnippetList,
}

var snippetsRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a saved snippet",
	Args:  cobra.ExactArgs(1),
	RunE:  runSnippetRemove,
}

func init() {
	snippetsSaveCmd.Flags().StringVar(&snippetSaveFormat, "format", "", "force format: text or png (auto-detected if empty)")
	snippetsSaveCmd.Flags().StringVar(&snippetSaveDescription, "description", "", "optional description of the snippet")
	snippetsCmd.AddCommand(snippetsSaveCmd)
	snippetsCmd.AddCommand(snippetsListCmd)
	snippetsCmd.AddCommand(snippetsRemoveCmd)
	rootCmd.AddCommand(snippetsCmd)
}

func runSnippetSave(cmd *cobra.Command, args []string) error {
	name := args[0]

	// Validate snippet name.
	if !isValidSnippetName(name) {
		return fail("invalid snippet name: %s (must be alphanumeric with - and _)", name)
	}

	// Check daemon is running.
	cfg, err := loadConfig()
	if err != nil {
		return fail("%v", err)
	}

	if !isDaemonRunning(cfg.Port) {
		return fail("daemon is not running. Start it with: pastelocal start")
	}

	// Fetch current clipboard.
	token, err := getToken()
	if err != nil {
		return fail("failed to get token: %v", err)
	}

	clipResp, err := fetchClipboard(cfg.Port, token)
	if err != nil {
		return fail("failed to fetch clipboard: %v", err)
	}

	// Determine format.
	format := snippetSaveFormat
	if format == "" {
		format = clipResp.Format
	}
	if format != "text" && format != "png" {
		return fail("unsupported format: %s (must be text or png)", format)
	}

	// Get data.
	var data []byte
	if format == "png" {
		data, err = base64.StdEncoding.DecodeString(clipResp.Image)
		if err != nil {
			return fail("failed to decode image: %v", err)
		}
	} else {
		data = []byte(clipResp.Text)
	}

	// Save snippet via API.
	if err := saveSnippet(cfg.Port, token, name, format, data, snippetSaveDescription); err != nil {
		return fail("failed to save snippet: %v", err)
	}

	fmt.Printf("Snippet '%s' saved (%s, %d bytes)\n", name, format, len(data))
	return nil
}

func runSnippetList(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return fail("%v", err)
	}

	token, err := getToken()
	if err != nil {
		return fail("failed to get token: %v", err)
	}

	snippets, err := listSnippets(cfg.Port, token)
	if err != nil {
		return fail("failed to list snippets: %v", err)
	}

	if len(snippets) == 0 {
		fmt.Println("No snippets saved. Use `pastelocal snippets save <name>` to create one.")
		return nil
	}

	fmt.Printf("%-20s %-10s %-10s %-20s %s\n", "NAME", "FORMAT", "SIZE", "UPDATED", "DESCRIPTION")
	for _, s := range snippets {
		desc := s.Description
		if len(desc) > 30 {
			desc = desc[:27] + "..."
		}
		fmt.Printf("%-20s %-10s %-10d %-20s %s\n", s.Name, s.Format, s.Size, s.UpdatedAt[:19], desc)
	}
	return nil
}

func runSnippetRemove(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg, err := loadConfig()
	if err != nil {
		return fail("%v", err)
	}

	token, err := getToken()
	if err != nil {
		return fail("failed to get token: %v", err)
	}

	if err := removeSnippet(cfg.Port, token, name); err != nil {
		return fail("failed to remove snippet: %v", err)
	}

	fmt.Printf("Snippet '%s' removed\n", name)
	return nil
}

// Helper functions.

func isValidSnippetName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func isDaemonRunning(port int) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func getToken() (string, error) {
	tokenPath := auth.DefaultTokenPath()
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func fetchClipboard(port int, token string) (*proto.ClipboardResponse, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/clipboard", port), nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var clipResp proto.ClipboardResponse
	if err := json.NewDecoder(resp.Body).Decode(&clipResp); err != nil {
		return nil, err
	}
	return &clipResp, nil
}

func saveSnippet(port int, token, name, format string, data []byte, description string) error {
	var reqBody proto.SnippetSaveRequest
	reqBody.Name = name
	reqBody.Format = format
	reqBody.Description = description
	if format == "png" {
		reqBody.Image = base64.StdEncoding.EncodeToString(data)
	} else {
		reqBody.Text = string(data)
	}

	body, _ := json.Marshal(reqBody)
	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest("POST", fmt.Sprintf("http://127.0.0.1:%d/snippets", port), strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	return nil
}

func listSnippets(port int, token string) ([]proto.SnippetEntry, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/snippets", port), nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var listResp proto.SnippetListResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, err
	}
	return listResp.Items, nil
}

func removeSnippet(port int, token, name string) error {
	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest("DELETE", fmt.Sprintf("http://127.0.0.1:%d/snippets/%s", port, name), nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	return nil
}

// ── relay / device pairing ───────────────────────────────────────────────────

var relayCmd = &cobra.Command{
	Use:   "relay",
	Short: "Manage E2E encrypted relay for multi-device sync",
	GroupID: "daemon",
}

var relayInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize device keypair for relay",
	RunE:  runRelayInit,
}

var relayPairCmd = &cobra.Command{
	Use:   "pair <relay-url>",
	Short: "Register device with relay server and get pairing code",
	Args:  cobra.ExactArgs(1),
	RunE:  runRelayPair,
}

var relayDevicesCmd = &cobra.Command{
	Use:   "devices",
	Short: "List all devices on the relay",
	RunE:  runRelayDevices,
}

var relayAddPeerCmd = &cobra.Command{
	Use:   "add-peer <peer-device-id>",
	Short: "Add a peer device for encrypted sharing",
	Args:  cobra.ExactArgs(1),
	RunE:  runRelayAddPeer,
}

var relaySendCmd = &cobra.Command{
	Use:   "send <peer-device-id-or-fp> [file]",
	Short: "Encrypt and send clipboard (or file) to a peer via relay",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runRelaySend,
}

var relayInboxCmd = &cobra.Command{
	Use:   "inbox",
	Short: "List pending items in your relay inbox",
	RunE:  runRelayInbox,
}

var relayFetchCmd = &cobra.Command{
	Use:   "fetch <sender-device-id-or-fp>",
	Short: "Fetch and decrypt a specific inbox item from a sender (writes temp file)",
	Args:  cobra.ExactArgs(1),
	RunE:  runRelayFetchCLI,
}

var relayStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show relay configuration, peers, and connectivity",
	RunE:  runRelayStatus,
}

var (
	relayFlagURL  string
	relayFlagTTL  int
	relaySendFile string
)

func init() {
	relayInitCmd.Flags().StringVar(&relayFlagURL, "relay-url", "http://localhost:7332", "relay server URL")
	relayPairCmd.Flags().StringVar(&relayFlagURL, "relay-url", "http://localhost:7332", "relay server URL")
	relayDevicesCmd.Flags().StringVar(&relayFlagURL, "relay-url", "http://localhost:7332", "relay server URL")
	relayAddPeerCmd.Flags().StringVar(&relayFlagURL, "relay-url", "http://localhost:7332", "relay server URL")
	relaySendCmd.Flags().StringVar(&relayFlagURL, "relay-url", "http://localhost:7332", "relay server URL")
	relaySendCmd.Flags().StringVar(&relaySendFile, "file", "", "file to send (default: current clipboard)")
	relayInboxCmd.Flags().StringVar(&relayFlagURL, "relay-url", "http://localhost:7332", "relay server URL")
	relayFetchCmd.Flags().StringVar(&relayFlagURL, "relay-url", "http://localhost:7332", "relay server URL")
	relayStatusCmd.Flags().StringVar(&relayFlagURL, "relay-url", "http://localhost:7332", "relay server URL")

	relayCmd.AddCommand(relayInitCmd)
	relayCmd.AddCommand(relayPairCmd)
	relayCmd.AddCommand(relayDevicesCmd)
	relayCmd.AddCommand(relayAddPeerCmd)
	relayCmd.AddCommand(relaySendCmd)
	relayCmd.AddCommand(relayInboxCmd)
	relayCmd.AddCommand(relayFetchCmd)
	relayCmd.AddCommand(relayStatusCmd)
	rootCmd.AddCommand(relayCmd)
}

func runRelayInit(cmd *cobra.Command, args []string) error {
	// Generate keypair.
	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		return fail("generating keypair: %v", err)
	}

	deviceID := kp.DeviceID()
	fingerprint := kp.Fingerprint()

	// Save keypair to file.
	keyPath := filepath.Join(expandHome("~/.config/pastelocal"), "device-key")
	if err := os.MkdirAll(filepath.Dir(keyPath), 0700); err != nil {
		return fail("creating directory: %v", err)
	}

	if err := os.WriteFile(keyPath, []byte(kp.PrivateKeyBase64()), 0600); err != nil {
		return fail("saving keypair: %v", err)
	}

	fmt.Printf("Device initialized successfully.\n")
	fmt.Printf("Device ID:     %s\n", deviceID)
	fmt.Printf("Fingerprint:   %s\n", fingerprint)
	fmt.Printf("Key saved to:  %s\n", keyPath)
	fmt.Printf("\nNext step: pastelocal relay pair <relay-url>\n")
	return nil
}

func runRelayPair(cmd *cobra.Command, args []string) error {
	url := args[0]

	// Load keypair.
	keyPath := filepath.Join(expandHome("~/.config/pastelocal"), "device-key")
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return fail("device key not found. Run 'pastelocal relay init' first: %v", err)
	}

	kp, err := crypto.LoadKeyPairFromBase64(string(keyData))
	if err != nil {
		return fail("loading keypair: %v", err)
	}

	deviceID := kp.DeviceID()
	fingerprint := kp.Fingerprint()

	// Create relay client and register.
	client := relay.NewClient(url, deviceID, kp, "")
	resp, err := client.Register()
	if err != nil {
		return fail("registering with relay: %v", err)
	}

	if !resp.OK {
		return fail("registration failed: %s", resp.Error)
	}

	fmt.Printf("Device registered successfully.\n")
	fmt.Printf("Device ID:       %s\n", deviceID)
	fmt.Printf("Fingerprint:     %s\n", fingerprint)
	fmt.Printf("Auth token:      %s\n", resp.Token)
	fmt.Printf("\nShare this fingerprint with peers to add them:\n")
	fmt.Printf("  %s\n", fingerprint)
	fmt.Printf("\nTo add a peer:\n")
	fmt.Printf("  pastelocal relay add-peer <peer-device-id>\n")

	// Save token.
	tokenPath := filepath.Join(expandHome("~/.config/pastelocal"), "relay-token")
	if err := os.WriteFile(tokenPath, []byte(resp.Token), 0600); err != nil {
		return fail("saving token: %v", err)
	}
	fmt.Printf("Token saved to:  %s\n", tokenPath)

	return nil
}

func runRelayDevices(cmd *cobra.Command, args []string) error {
	// Load keypair.
	keyPath := filepath.Join(expandHome("~/.config/pastelocal"), "device-key")
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return fail("device key not found. Run 'pastelocal relay init' first: %v", err)
	}

	kp, err := crypto.LoadKeyPairFromBase64(string(keyData))
	if err != nil {
		return fail("loading keypair: %v", err)
	}

	// Load token.
	tokenPath := filepath.Join(expandHome("~/.config/pastelocal"), "relay-token")
	tokenData, err := os.ReadFile(tokenPath)
	if err != nil {
		return fail("relay token not found. Run 'pastelocal relay pair' first: %v", err)
	}

	client := relay.NewClient(relayFlagURL, kp.DeviceID(), kp, string(tokenData))
	resp, err := client.ListDevices()
	if err != nil {
		return fail("listing devices: %v", err)
	}

	if !resp.OK {
		return fail("listing devices failed: %s", resp.Error)
	}

	if len(resp.Devices) == 0 {
		fmt.Println("No devices registered on relay.")
		return nil
	}

	fmt.Printf("%-40s %-20s %s\n", "DEVICE ID", "FINGERPRINT", "LAST SEEN")
	for _, dev := range resp.Devices {
		lastSeen := time.Unix(dev.LastSeen, 0).Format("2006-01-02 15:04:05")
		fmt.Printf("%-40s %-20s %s\n", dev.DeviceID, dev.Fingerprint, lastSeen)
	}

	return nil
}

func runRelayAddPeer(cmd *cobra.Command, args []string) error {
	peerDeviceID := args[0]

	// Load keypair.
	keyPath := filepath.Join(expandHome("~/.config/pastelocal"), "device-key")
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return fail("device key not found. Run 'pastelocal relay init' first: %v", err)
	}

	kp, err := crypto.LoadKeyPairFromBase64(string(keyData))
	if err != nil {
		return fail("loading keypair: %v", err)
	}

	// Load token.
	tokenPath := filepath.Join(expandHome("~/.config/pastelocal"), "relay-token")
	tokenData, err := os.ReadFile(tokenPath)
	if err != nil {
		return fail("relay token not found. Run 'pastelocal relay pair' first: %v", err)
	}

	client := relay.NewClient(relayFlagURL, kp.DeviceID(), kp, string(tokenData))
	resp, err := client.AddPeer(peerDeviceID)
	if err != nil {
		return fail("adding peer: %v", err)
	}

	if !resp.OK {
		return fail("adding peer failed: %s", resp.Error)
	}

	fmt.Printf("Peer added: %s\n", peerDeviceID)
	return nil
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}

// runRelaySend implements `pastelocal relay send <peer> [--file F]`
func runRelaySend(cmd *cobra.Command, args []string) error {
	peer := args[0]
	if len(args) > 1 && relaySendFile == "" {
		relaySendFile = args[1]
	}

	keyPath := filepath.Join(expandHome("~/.config/pastelocal"), "device-key")
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return fail("device key not found. Run 'pastelocal relay init' first: %v", err)
	}
	kp, err := crypto.LoadKeyPairFromBase64(string(keyData))
	if err != nil {
		return fail("loading keypair: %v", err)
	}

	tokenPath := filepath.Join(expandHome("~/.config/pastelocal"), "relay-token")
	tokenData, err := os.ReadFile(tokenPath)
	if err != nil {
		return fail("relay token not found. Run 'pastelocal relay pair' first: %v", err)
	}

	client := relay.NewClient(relayFlagURL, kp.DeviceID(), kp, string(tokenData))

	var data []byte
	format := "text"
	if relaySendFile != "" {
		data, err = os.ReadFile(relaySendFile)
		if err != nil {
			return fail("reading file: %v", err)
		}
		if len(data) > 8 && data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G' {
			format = "png"
		}
	} else {
		// Read local clipboard (best effort on this host)
		// Use a simple approach to avoid heavy deps in control CLI for v1
		// (user can always use --file or the watcher auto path)
		return fail("no --file provided and clipboard read from 'pastelocal relay send' not wired for v1 (use the daemon watcher or pastelocal-remote --send for now)")
	}

	// Resolve peer pubkey
	var pubB64 string
	var targetID = peer
	peersResp, _ := client.ListPeers()
	if peersResp != nil && peersResp.OK {
		for _, p := range peersResp.Peers {
			if p.DeviceID == peer || p.Fingerprint == peer || strings.HasPrefix(p.DeviceID, peer) {
				pubB64 = p.PublicKey
				targetID = p.DeviceID
				break
			}
		}
	}
	if pubB64 == "" {
		devs, _ := client.ListDevices()
		if devs != nil && devs.OK {
			for _, d := range devs.Devices {
				if d.DeviceID == peer || d.Fingerprint == peer || strings.HasPrefix(d.DeviceID, peer) {
					pubB64 = d.PublicKey
					targetID = d.DeviceID
					break
				}
			}
		}
	}
	if pubB64 == "" {
		return fail("peer %s not found or has no pubkey (add-peer both directions first)", peer)
	}

	pub, err := crypto.ParsePublicKey(pubB64)
	if err != nil {
		return fail("parsing peer pubkey: %v", err)
	}

	_, err = client.EncryptAndUploadTo(pub, targetID, format, data, 300)
	if err != nil {
		return fail("send via relay: %v", err)
	}
	fmt.Printf("Sent to peer %s via relay (format=%s, %d bytes)\n", targetID, format, len(data))
	return nil
}

// runRelayInbox lists pending relay inbox items.
func runRelayInbox(cmd *cobra.Command, args []string) error {
	keyPath := filepath.Join(expandHome("~/.config/pastelocal"), "device-key")
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return fail("device key not found. Run 'pastelocal relay init' first: %v", err)
	}
	kp, err := crypto.LoadKeyPairFromBase64(string(keyData))
	if err != nil {
		return fail("loading keypair: %v", err)
	}
	tokenPath := filepath.Join(expandHome("~/.config/pastelocal"), "relay-token")
	tokenData, err := os.ReadFile(tokenPath)
	if err != nil {
		return fail("relay token not found. Run 'pastelocal relay pair' first: %v", err)
	}

	client := relay.NewClient(relayFlagURL, kp.DeviceID(), kp, string(tokenData))
	resp, err := client.ListInbox()
	if err != nil {
		return fail("listing inbox: %v", err)
	}
	if !resp.OK {
		return fail("inbox error: %s", resp.Error)
	}
	if len(resp.Pending) == 0 {
		fmt.Println("Inbox empty.")
		return nil
	}
	fmt.Printf("%-20s %-12s %s\n", "SENDER FP", "FORMAT", "TIME")
	for _, it := range resp.Pending {
		t := time.Unix(it.Timestamp, 0).Format("2006-01-02 15:04")
		fmt.Printf("%-20s %-12s %s\n", it.Fingerprint, it.Format, t)
	}
	fmt.Println("\nUse 'pastelocal relay fetch <sender-id>' to retrieve one.")
	return nil
}

// runRelayFetchCLI fetches one inbox item and writes it (like remote).
func runRelayFetchCLI(cmd *cobra.Command, args []string) error {
	sender := args[0]

	keyPath := filepath.Join(expandHome("~/.config/pastelocal"), "device-key")
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return fail("device key not found. Run 'pastelocal relay init' first: %v", err)
	}
	kp, err := crypto.LoadKeyPairFromBase64(string(keyData))
	if err != nil {
		return fail("loading keypair: %v", err)
	}
	tokenPath := filepath.Join(expandHome("~/.config/pastelocal"), "relay-token")
	tokenData, err := os.ReadFile(tokenPath)
	if err != nil {
		return fail("relay token not found. Run 'pastelocal relay pair' first: %v", err)
	}

	client := relay.NewClient(relayFlagURL, kp.DeviceID(), kp, string(tokenData))

	// normalize sender if fingerprint etc by listing
	senderID := sender
	// try list inbox to get full id if fp given, or just use
	download, err := client.FetchFromInbox(senderID)
	if err != nil || !download.OK {
		// try resolve via devices
		devs, _ := client.ListDevices()
		if devs != nil {
			for _, d := range devs.Devices {
				if d.Fingerprint == sender || strings.HasPrefix(d.DeviceID, sender) {
					senderID = d.DeviceID
					break
				}
			}
		}
		download, err = client.FetchFromInbox(senderID)
	}
	if err != nil {
		return fail("fetch: %v", err)
	}
	if !download.OK {
		return fail("fetch error: %s", download.Error)
	}

	// resolve pubkey
	pubB64 := ""
	devs, _ := client.ListDevices()
	if devs != nil && devs.OK {
		for _, d := range devs.Devices {
			if d.DeviceID == senderID {
				pubB64 = d.PublicKey
				break
			}
		}
	}
	if pubB64 == "" {
		return fail("no pubkey for sender %s", senderID)
	}
	pub, err := crypto.ParsePublicKey(pubB64)
	if err != nil {
		return fail("parse pubkey: %v", err)
	}

	enc, err := base64.StdEncoding.DecodeString(download.Data)
	if err != nil {
		return fail("decode data: %v", err)
	}
	non, err := base64.StdEncoding.DecodeString(download.Nonce)
	if err != nil {
		return fail("decode nonce: %v", err)
	}
	shared, err := kp.SharedSecret(pub)
	if err != nil {
		return fail("shared secret: %v", err)
	}
	plain, err := crypto.Decrypt(enc, shared, non)
	if err != nil {
		return fail("decrypt: %v", err)
	}

	// write temp like remote does (simplified)
	outDir := expandHome("~/.cache/pastelocal")
	os.MkdirAll(outDir, 0700)
	ext := "bin"
	if download.Format == "png" {
		ext = "png"
	}
	path := filepath.Join(outDir, fmt.Sprintf("relay-%s-%d.%s", senderID[:8], time.Now().Unix(), ext))
	if err := os.WriteFile(path, plain, 0600); err != nil {
		return fail("write: %v", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	fmt.Println(abs)
	return nil
}

// runRelayStatus shows relay health for the local device.
func runRelayStatus(cmd *cobra.Command, args []string) error {
	keyPath := filepath.Join(expandHome("~/.config/pastelocal"), "device-key")
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		fmt.Println("No device key (run 'pastelocal relay init').")
		return nil
	}
	kp, _ := crypto.LoadKeyPairFromBase64(string(keyData))
	fmt.Printf("Device ID:   %s\n", kp.DeviceID())
	fmt.Printf("Fingerprint: %s\n", kp.Fingerprint())

	tokenPath := filepath.Join(expandHome("~/.config/pastelocal"), "relay-token")
	if td, err := os.ReadFile(tokenPath); err == nil {
		fmt.Printf("Token:       %s\n", strings.TrimSpace(string(td)))
	}

	client := relay.NewClient(relayFlagURL, kp.DeviceID(), kp, "")
	// try unauth health
	// but for simplicity just try list devices (requires token? for now load token if present)
	if td, err := os.ReadFile(tokenPath); err == nil {
		client = relay.NewClient(relayFlagURL, kp.DeviceID(), kp, strings.TrimSpace(string(td)))
		resp, err := client.ListDevices()
		if err == nil && resp.OK {
			fmt.Printf("Relay at %s: %d devices registered\n", relayFlagURL, len(resp.Devices))
		} else {
			fmt.Printf("Relay %s: %v\n", relayFlagURL, err)
		}
		peers, _ := client.ListPeers()
		if peers != nil && peers.OK {
			fmt.Printf("Your peers: %d\n", len(peers.Peers))
		}
	}
	fmt.Println("Auto-upload in daemon: check ~/.config/pastelocal/config.toml [relay] auto_upload = true")
	return nil
}
