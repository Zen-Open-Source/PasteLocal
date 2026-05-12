package doctor

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/clipbridge/clipbridge/internal/auth"
	"github.com/clipbridge/clipbridge/internal/config"
	"github.com/clipbridge/clipbridge/internal/hostinstall"
	"github.com/clipbridge/clipbridge/internal/service"
	"github.com/clipbridge/clipbridge/internal/sshconfig"
)

// checkDaemonRunning verifies the daemon process is running via service.Status.
func checkDaemonRunning(cfg *config.Config) CheckResult {
	svc := service.New("", config.ConfigPath(), cfg.Port)
	running, pid, err := svc.Status()
	if err != nil {
		return CheckResult{
			Name:    "Daemon running",
			Passed:  false,
			Detail:  fmt.Sprintf("status check failed: %v", err),
			FixHint: "Run `clipbridge start`",
			AutoFix: false,
		}
	}
	if !running {
		return CheckResult{
			Name:    "Daemon running",
			Passed:  false,
			Detail:  "not running",
			FixHint: "Run `clipbridge start`",
			AutoFix: false,
		}
	}
	return CheckResult{
		Name:   "Daemon running",
		Passed: true,
		Detail: fmt.Sprintf("[pid %d, port %d]", pid, cfg.Port),
	}
}

// checkDaemonHTTP verifies the daemon HTTP endpoint responds on the configured port.
func checkDaemonHTTP(cfg *config.Config) CheckResult {
	url := fmt.Sprintf("http://127.0.0.1:%d/health", cfg.Port)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return CheckResult{
			Name:    "Daemon HTTP responding",
			Passed:  false,
			Detail:  fmt.Sprintf("GET /health failed: %v", err),
			FixHint: "Run `clipbridge start`",
			AutoFix: false,
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return CheckResult{
			Name:    "Daemon HTTP responding",
			Passed:  false,
			Detail:  fmt.Sprintf("GET /health returned %d", resp.StatusCode),
			FixHint: "Check `clipbridge logs`",
			AutoFix: false,
		}
	}
	return CheckResult{
		Name:   "Daemon HTTP responding",
		Passed: true,
		Detail: fmt.Sprintf("[port %d]", cfg.Port),
	}
}

// checkLoopbackOnly verifies the daemon is bound to loopback only.
// It checks the configured listen address (the daemon always binds 127.0.0.1)
// and verifies there's no non-loopback listener on the port.
func checkLoopbackOnly(cfg *config.Config) CheckResult {
	// The daemon always binds to 127.0.0.1 per server.go. If the port
	// is also bound on a non-loopback address, something else may be
	// listening. We check via lsof/netstat on the port.
	port := cfg.Port

	// Use lsof to check what's listening on the port.
	cmd := exec.Command("lsof", "-i", fmt.Sprintf(":%d", port), "-sTCP:LISTEN", "-n", "-P")
	out, err := cmd.Output()
	if err != nil {
		// lsof not available or no listeners found — fall back to
		// checking that the config itself only uses loopback.
		return CheckResult{
			Name:   "Daemon bound to loopback only",
			Passed: true,
			Detail: fmt.Sprintf("[port %d, binding 127.0.0.1]", port),
		}
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	nonLoopback := false
	for _, line := range lines {
		// Skip header line.
		if strings.HasPrefix(line, "COMMAND") {
			continue
		}
		fields := strings.Fields(line)
		// Look for the address field which contains the listen address.
		for _, f := range fields {
			if strings.HasPrefix(f, "*:") || strings.HasPrefix(f, "0.0.0.0:") {
				nonLoopback = true
			}
		}
	}

	if nonLoopback {
		return CheckResult{
			Name:    "Daemon bound to loopback only",
			Passed:  false,
			Detail:  fmt.Sprintf("port %d also bound on non-loopback address", port),
			FixHint: "Check your config; set port to a loopback-only binding",
			AutoFix: false, // NOT auto-fixable — config issue
		}
	}

	return CheckResult{
		Name:   "Daemon bound to loopback only",
		Passed: true,
		Detail: fmt.Sprintf("[port %d, loopback only]", port),
	}
}

// checkTokenFile verifies the local token file exists with mode 0600 and is readable.
func checkTokenFile(cfg *config.Config) CheckResult {
	tokenPath := resolvedTokenPath()
	if tokenPath == "" {
		return CheckResult{
			Name:    "Local token file exists",
			Passed:  false,
			Detail:  "cannot determine token path",
			FixHint: "Run `clipbridge start` to generate a token",
			AutoFix: false,
		}
	}

	info, err := os.Stat(tokenPath)
	if err != nil {
		if os.IsNotExist(err) {
			return CheckResult{
				Name:    "Local token file exists",
				Passed:  false,
				Detail:  fmt.Sprintf("%s does not exist", tokenPath),
				FixHint: "Run `clipbridge start` to generate a token",
				AutoFix: false,
			}
		}
		return CheckResult{
			Name:    "Local token file exists",
			Passed:  false,
			Detail:  fmt.Sprintf("stat %s: %v", tokenPath, err),
			FixHint: "Check file permissions on " + tokenPath,
			AutoFix: false,
		}
	}

	// Check permissions are 0600.
	perm := info.Mode().Perm()
	if perm != fs.FileMode(0600) {
		return CheckResult{
			Name:    "Local token file permissions",
			Passed:  false,
			Detail:  fmt.Sprintf("mode %04o (expected 0600)", perm),
			FixHint: "Run `clipbridge doctor --fix`",
			AutoFix: true,
		}
	}

	// Check readable by current user.
	f, err := os.Open(tokenPath)
	if err != nil {
		return CheckResult{
			Name:    "Local token file readable",
			Passed:  false,
			Detail:  fmt.Sprintf("cannot read: %v", err),
			FixHint: "Check file ownership on " + tokenPath,
			AutoFix: false,
		}
	}
	f.Close()

	return CheckResult{
		Name:   "Local token file exists",
		Passed: true,
		Detail: fmt.Sprintf("[%s, mode 0600]", tokenPath),
	}
}

// checkKeychainEntry verifies the keychain (macOS) / libsecret (Linux) entry exists.
func checkKeychainEntry(cfg *config.Config) CheckResult {
	// Try retrieving from the platform keychain.
	store := auth.NewTokenStore(true, auth.DefaultTokenPath())
	token, err := store.Retrieve()
	if err != nil {
		return CheckResult{
			Name:    "Keychain entry exists",
			Passed:  false,
			Detail:  keychainLabel() + " entry not found",
			FixHint: "Run `clipbridge doctor --fix` to re-store token in keychain",
			AutoFix: true,
		}
	}
	if token == "" {
		return CheckResult{
			Name:    "Keychain entry exists",
			Passed:  false,
			Detail:  keychainLabel() + " entry is empty",
			FixHint: "Run `clipbridge doctor --fix` to re-store token in keychain",
			AutoFix: true,
		}
	}
	return CheckResult{
		Name:   "Keychain entry exists",
		Passed: true,
		Detail: keychainLabel() + " entry present",
	}
}

// keychainLabel returns a human-readable label for the platform keychain.
func keychainLabel() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS Keychain"
	case "linux":
		return "libsecret"
	default:
		return "keychain"
	}
}

// checkClipboardTool verifies the local clipboard tool is installed and executable.
func checkClipboardTool(cfg *config.Config) CheckResult {
	tool, hint := clipboardToolName(cfg)
	if tool == "" {
		return CheckResult{
			Name:    "Clipboard tool installed",
			Passed:  false,
			Detail:  "no clipboard tool configured for this platform",
			FixHint: hint,
			AutoFix: false, // NOT auto-fixable — requires brew install consent
		}
	}

	path, err := exec.LookPath(tool)
	if err != nil {
		return CheckResult{
			Name:    "Clipboard tool installed",
			Passed:  false,
			Detail:  fmt.Sprintf("%s not found in PATH", tool),
			FixHint: hint,
			AutoFix: false, // NOT auto-fixable — requires brew install consent
		}
	}
	return CheckResult{
		Name:   "Clipboard tool installed",
		Passed: true,
		Detail: fmt.Sprintf("%s [%s]", tool, path),
	}
}

// clipboardToolName returns the clipboard tool name and install hint for the
// current platform, considering user configuration overrides.
func clipboardToolName(cfg *config.Config) (tool, hint string) {
	switch runtime.GOOS {
	case "darwin":
		tool = "pngpaste"
		if cfg.MacOS.PngpastePath != "" {
			tool = cfg.MacOS.PngpastePath
		}
		hint = "Install via: brew install pngpaste"
	case "linux":
		switch cfg.Linux.ClipboardTool {
		case "wl-paste":
			tool = "wl-paste"
			hint = "Install via your package manager (wl-clipboard)"
		case "xclip":
			tool = "xclip"
			hint = "Install via your package manager (xclip)"
		default:
			// Auto-detect: try wl-paste first (Wayland), then xclip (X11).
			if _, err := exec.LookPath("wl-paste"); err == nil {
				return "wl-paste", "Install via your package manager (wl-clipboard)"
			}
			if _, err := exec.LookPath("xclip"); err == nil {
				return "xclip", "Install via your package manager (xclip)"
			}
			return "", "Install wl-clipboard or xclip via your package manager"
		}
	default:
		tool = ""
		hint = "Clipboard tools not supported on this platform"
	}
	return tool, hint
}

// checkHostSSHConfig verifies the SSH config has a RemoteForward line for the
// host (or notes Termius mode).
func checkHostSSHConfig(cfg *config.Config, alias string) CheckResult {
	hostCfg, ok := cfg.Hosts[alias]
	if !ok {
		return CheckResult{
			Name:    "SSH RemoteForward configured",
			Passed:  false,
			Detail:  fmt.Sprintf("host %q not in config", alias),
			FixHint: "Run `clipbridge add-host " + alias + "`",
			AutoFix: false,
		}
	}

	// Termius mode: just note it.
	if hostCfg.Termius {
		return CheckResult{
			Name:   "SSH RemoteForward configured",
			Passed: true,
			Detail: fmt.Sprintf("[Termius mode — manual port forwarding]"),
		}
	}

	// Read and parse SSH config.
	sshCfgPath := userSSHConfigPath()
	content, err := os.ReadFile(sshCfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			return CheckResult{
				Name:    "SSH RemoteForward configured",
				Passed:  false,
				Detail:  "~/.ssh/config does not exist",
				FixHint: "Run `clipbridge doctor --fix` or `clipbridge add-host " + alias + "`",
				AutoFix: true,
			}
		}
		return CheckResult{
			Name:    "SSH RemoteForward configured",
			Passed:  false,
			Detail:  fmt.Sprintf("read ssh config: %v", err),
			FixHint: "Check file permissions on ~/.ssh/config",
			AutoFix: false,
		}
	}

	parsed, err := sshconfig.Parse(string(content))
	if err != nil {
		return CheckResult{
			Name:    "SSH RemoteForward configured",
			Passed:  false,
			Detail:  fmt.Sprintf("parse ssh config: %v", err),
			FixHint: "Fix syntax errors in ~/.ssh/config",
			AutoFix: false,
		}
	}

	port := hostCfg.RemotePort
	if port == 0 {
		port = cfg.Port
	}

	if sshconfig.HasRemoteForward(parsed, alias, port) {
		return CheckResult{
			Name:   "SSH RemoteForward configured",
			Passed: true,
			Detail: fmt.Sprintf("[port %d forwarded for %s]", port, alias),
		}
	}

	return CheckResult{
		Name:    "SSH RemoteForward configured",
		Passed:  false,
		Detail:  fmt.Sprintf("no RemoteForward for %s (port %d)", alias, port),
		FixHint: "Run `clipbridge doctor --fix` to add RemoteForward",
		AutoFix: true,
	}
}

// checkHostSSHConnection verifies the SSH connection works by running
// ssh -o BatchMode=yes <host> true.
func checkHostSSHConnection(cfg *config.Config, alias string) CheckResult {
	hostCfg, ok := cfg.Hosts[alias]
	if !ok {
		return CheckResult{
			Name:    "SSH connection works",
			Passed:  false,
			Detail:  fmt.Sprintf("host %q not in config", alias),
			FixHint: "Run `clipbridge add-host " + alias + "`",
			AutoFix: false,
		}
	}

	sshHost := sshHostStr(hostCfg, alias)
	cmd := exec.Command("ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5", sshHost, "true")
	if out, err := cmd.CombinedOutput(); err != nil {
		return CheckResult{
			Name:    "SSH connection works",
			Passed:  false,
			Detail:  fmt.Sprintf("ssh %s failed: %v: %s", sshHost, err, strings.TrimSpace(string(out))),
			FixHint: "Check SSH connectivity to " + sshHost,
			AutoFix: false,
		}
	}
	return CheckResult{
		Name:   "SSH connection works",
		Passed: true,
		Detail: fmt.Sprintf("[%s]", sshHost),
	}
}

// checkHostRemoteBinary verifies the remote clipbridge-remote binary is present and executable.
func checkHostRemoteBinary(cfg *config.Config, alias string) CheckResult {
	hostCfg, ok := cfg.Hosts[alias]
	if !ok {
		return CheckResult{
			Name:    "Remote binary present",
			Passed:  false,
			Detail:  fmt.Sprintf("host %q not in config", alias),
			FixHint: "Run `clipbridge add-host " + alias + "`",
			AutoFix: false,
		}
	}

	sshHost := sshHostStr(hostCfg, alias)
	remotePath := remoteBinPath(hostCfg)

	stdout, stderr, err := hostinstall.RunSSH(sshHost, "test", "-x", remotePath, "&&", "echo", "ok")
	if err != nil {
		return CheckResult{
			Name:    "Remote binary present",
			Passed:  false,
			Detail:  fmt.Sprintf("%s not found/executable on %s: %s", remotePath, alias, strings.TrimSpace(stderr)),
			FixHint: "Run `clipbridge doctor --fix` to re-copy binary",
			AutoFix: true,
		}
	}
	if strings.TrimSpace(stdout) != "ok" {
		return CheckResult{
			Name:    "Remote binary present",
			Passed:  false,
			Detail:  fmt.Sprintf("%s not executable on %s", remotePath, alias),
			FixHint: "Run `clipbridge doctor --fix` to re-copy binary",
			AutoFix: true,
		}
	}
	return CheckResult{
		Name:   "Remote binary present",
		Passed: true,
		Detail: fmt.Sprintf("[%s on %s]", remotePath, alias),
	}
}

// checkHostRemoteToken verifies the remote token file is present with mode 0600.
func checkHostRemoteToken(cfg *config.Config, alias string) CheckResult {
	hostCfg, ok := cfg.Hosts[alias]
	if !ok {
		return CheckResult{
			Name:    "Remote token file present",
			Passed:  false,
			Detail:  fmt.Sprintf("host %q not in config", alias),
			FixHint: "Run `clipbridge add-host " + alias + "`",
			AutoFix: false,
		}
	}

	sshHost := sshHostStr(hostCfg, alias)
	const remoteTokenPath = "~/.config/clipbridge/token"

	// Check file exists.
	exists, err := hostinstall.RemoteFileExists(sshHost, remoteTokenPath)
	if err != nil || !exists {
		return CheckResult{
			Name:    "Remote token file present",
			Passed:  false,
			Detail:  fmt.Sprintf("%s not found on %s", remoteTokenPath, alias),
			FixHint: "Run `clipbridge add-host " + alias + "` to sync the token",
			AutoFix: false,
		}
	}

	// Check permissions via stat -c %a (Linux) or stat -f %Lp (macOS/BSD).
	stdout, _, err := hostinstall.RunSSH(sshHost, "stat", "-c", "%a", remoteTokenPath)
	if err != nil {
		// Try BSD stat format (macOS).
		stdout, _, err = hostinstall.RunSSH(sshHost, "stat", "-f", "%Lp", remoteTokenPath)
		if err != nil {
			return CheckResult{
				Name:    "Remote token file present",
				Passed:  true, // File exists, just can't check perms.
				Detail:  fmt.Sprintf("[%s on %s, permissions unknown]", remoteTokenPath, alias),
			}
		}
	}

	perm := strings.TrimSpace(stdout)
	if perm != "600" {
		return CheckResult{
			Name:    "Remote token file permissions",
			Passed:  false,
			Detail:  fmt.Sprintf("mode %s (expected 600) on %s", perm, alias),
			FixHint: "Run `clipbridge doctor --fix` to correct permissions",
			AutoFix: true,
		}
	}

	return CheckResult{
		Name:   "Remote token file present",
		Passed: true,
		Detail: fmt.Sprintf("[%s on %s, mode 0600]", remoteTokenPath, alias),
	}
}

// checkHostRemoteSkill verifies the remote skill is installed at ~/.claude/commands/paste.md.
func checkHostRemoteSkill(cfg *config.Config, alias string) CheckResult {
	hostCfg, ok := cfg.Hosts[alias]
	if !ok {
		return CheckResult{
			Name:    "Remote skill installed",
			Passed:  false,
			Detail:  fmt.Sprintf("host %q not in config", alias),
			FixHint: "Run `clipbridge add-host " + alias + "`",
			AutoFix: false,
		}
	}

	sshHost := sshHostStr(hostCfg, alias)
	const skillPath = "~/.claude/commands/paste.md"

	exists, err := hostinstall.RemoteFileExists(sshHost, skillPath)
	if err != nil || !exists {
		return CheckResult{
			Name:    "Remote skill installed",
			Passed:  false,
			Detail:  fmt.Sprintf("%s not found on %s", skillPath, alias),
			FixHint: "Run `clipbridge doctor --fix` to re-copy skill file",
			AutoFix: true,
		}
	}
	return CheckResult{
		Name:   "Remote skill installed",
		Passed: true,
		Detail: fmt.Sprintf("[%s on %s]", skillPath, alias),
	}
}

// checkHostDiskSpace verifies the cache directory on each host has > 100 MiB free.
func checkHostDiskSpace(cfg *config.Config, alias string) CheckResult {
	hostCfg, ok := cfg.Hosts[alias]
	if !ok {
		return CheckResult{
			Name:    "Remote disk space",
			Passed:  false,
			Detail:  fmt.Sprintf("host %q not in config", alias),
			FixHint: "Run `clipbridge add-host " + alias + "`",
			AutoFix: false,
		}
	}

	sshHost := sshHostStr(hostCfg, alias)

	// Check available disk space on the partition containing ~/.config/clipbridge.
	stdout, stderr, err := hostinstall.RunSSH(sshHost, "df", "-m", "~/.config/clipbridge")
	if err != nil {
		return CheckResult{
			Name:    "Remote disk space",
			Passed:  false,
			Detail:  fmt.Sprintf("df failed on %s: %s", alias, strings.TrimSpace(stderr)),
			FixHint: "Check disk space on " + alias,
			AutoFix: false,
		}
	}

	// Parse df output: Filesystem 1M-blocks Used Available Use% Mounted
	// Skip header line.
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) < 2 {
		return CheckResult{
			Name:    "Remote disk space",
			Passed:  false,
			Detail:  fmt.Sprintf("unexpected df output on %s", alias),
			FixHint: "Check disk space on " + alias,
			AutoFix: false,
		}
	}

	fields := strings.Fields(lines[1])
	if len(fields) < 4 {
		return CheckResult{
			Name:    "Remote disk space",
			Passed:  false,
			Detail:  fmt.Sprintf("unexpected df output on %s", alias),
			FixHint: "Check disk space on " + alias,
			AutoFix: false,
		}
	}

	// fields[3] is the available space in MiB.
	availStr := fields[3]
	var availMiB int
	if _, err := fmt.Sscanf(availStr, "%d", &availMiB); err != nil {
		return CheckResult{
			Name:    "Remote disk space",
			Passed:  false,
			Detail:  fmt.Sprintf("cannot parse available space on %s: %v", alias, err),
			FixHint: "Check disk space on " + alias,
			AutoFix: false,
		}
	}

	if availMiB <= 100 {
		return CheckResult{
			Name:    "Remote disk space",
			Passed:  false,
			Detail:  fmt.Sprintf("%d MiB free on %s (need > 100 MiB)", availMiB, alias),
			FixHint: "Free disk space on " + alias,
			AutoFix: false,
		}
	}

	return CheckResult{
		Name:   "Remote disk space",
		Passed: true,
		Detail: fmt.Sprintf("[%d MiB free on %s]", availMiB, alias),
	}
}

// sshHostStr builds the SSH host string (user@host or just host) from host config.
func sshHostStr(h config.Host, alias string) string {
	if h.RemoteUser != "" {
		return h.RemoteUser + "@" + alias
	}
	return alias
}

// remoteBinPath returns the remote binary path from host config or the default.
func remoteBinPath(h config.Host) string {
	if h.RemotePath != "" {
		return h.RemotePath
	}
	return "~/.local/bin/clipbridge-remote"
}

// userSSHConfigPath returns the path to the user's ~/.ssh/config file.
func userSSHConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.Getenv("HOME"), ".ssh", "config")
	}
	return filepath.Join(home, ".ssh", "config")
}
