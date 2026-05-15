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
	"github.com/pastelocal/pastelocal/internal/doctor"
	"github.com/pastelocal/pastelocal/internal/hostinstall"
	"github.com/pastelocal/pastelocal/internal/proto"
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
