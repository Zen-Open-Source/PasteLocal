// Package doctor implements diagnostic checks for the
// `clipbridge doctor` command. It verifies that the daemon is running,
// the token store is healthy, the clipboard tool is available, and each
// configured host is reachable with the correct remote setup.
package doctor

import (
	"fmt"
	"io"
	"os"

	"github.com/clipbridge/clipbridge/internal/config"
)

// CheckResult represents a single diagnostic check result.
type CheckResult struct {
	Name    string // e.g., "Daemon running"
	Passed  bool
	Detail  string // e.g., "[pid 12345, port 7331]"
	FixHint string // e.g., "Run `clipbridge start`"
	AutoFix bool   // whether --fix can handle this
}

// String formats the check result for terminal output.
// Pass: "✓ <name>  <detail>"
// Fail: "✗ <name>  <detail>" then newline + "  FIX: <fix_hint>"
func (r CheckResult) String() string {
	if r.Passed {
		return fmt.Sprintf("✓ %s  %s", r.Name, r.Detail)
	}
	if r.FixHint != "" {
		return fmt.Sprintf("✗ %s  %s\n  FIX: %s", r.Name, r.Detail, r.FixHint)
	}
	return fmt.Sprintf("✗ %s  %s", r.Name, r.Detail)
}

// RunChecks runs all diagnostic checks and returns results.
// If fix is true, auto-fixable failures are repaired automatically.
func RunChecks(cfg *config.Config, fix bool) []CheckResult {
	var results []CheckResult

	// Local checks (spec §11.3 items 1–7).
	results = append(results, checkDaemonRunning(cfg))
	results = append(results, checkDaemonHTTP(cfg))
	results = append(results, checkLoopbackOnly(cfg))
	results = append(results, checkTokenFile(cfg))
	results = append(results, checkKeychainEntry(cfg))
	results = append(results, checkClipboardTool(cfg))

	// Per-host checks (spec §11.3 items 8–9).
	for alias := range cfg.Hosts {
		results = append(results, RunHostChecks(cfg, alias, fix)...)
	}

	// Apply auto-fixes if requested.
	if fix {
		applyFixes(cfg, results)
		// Re-run checks after fixing to reflect the new state.
		results = nil
		results = append(results, checkDaemonRunning(cfg))
		results = append(results, checkDaemonHTTP(cfg))
		results = append(results, checkLoopbackOnly(cfg))
		results = append(results, checkTokenFile(cfg))
		results = append(results, checkKeychainEntry(cfg))
		results = append(results, checkClipboardTool(cfg))
		for alias := range cfg.Hosts {
			results = append(results, RunHostChecks(cfg, alias, false)...)
		}
	}

	return results
}

// RunHostChecks runs checks for a specific host.
// If fix is true, auto-fixable host failures are repaired automatically.
func RunHostChecks(cfg *config.Config, alias string, fix bool) []CheckResult {
	var results []CheckResult

	results = append(results, checkHostSSHConfig(cfg, alias))
	results = append(results, checkHostSSHConnection(cfg, alias))
	results = append(results, checkHostRemoteBinary(cfg, alias))
	results = append(results, checkHostRemoteToken(cfg, alias))
	results = append(results, checkHostRemoteSkill(cfg, alias))
	results = append(results, checkHostDiskSpace(cfg, alias))

	// Apply auto-fixes for this host if requested.
	if fix {
		applyHostFixes(cfg, alias, results)
		// Re-run host checks after fixing.
		results = nil
		results = append(results, checkHostSSHConfig(cfg, alias))
		results = append(results, checkHostSSHConnection(cfg, alias))
		results = append(results, checkHostRemoteBinary(cfg, alias))
		results = append(results, checkHostRemoteToken(cfg, alias))
		results = append(results, checkHostRemoteSkill(cfg, alias))
		results = append(results, checkHostDiskSpace(cfg, alias))
	}

	return results
}

// PrintResults writes check results to w in human-readable format.
func PrintResults(w io.Writer, results []CheckResult) {
	for _, r := range results {
		fmt.Fprintln(w, r.String())
	}
}

// applyFixes attempts auto-fix for each fixable, failed check.
func applyFixes(cfg *config.Config, results []CheckResult) {
	for i, r := range results {
		if r.Passed || !r.AutoFix {
			continue
		}
		if err := runAutoFix(cfg, "", r.Name); err != nil {
			results[i].Detail = fmt.Sprintf("%s (fix failed: %v)", r.Detail, err)
		}
	}
}

// applyHostFixes attempts auto-fix for each fixable, failed host check.
func applyHostFixes(cfg *config.Config, alias string, results []CheckResult) {
	for i, r := range results {
		if r.Passed || !r.AutoFix {
			continue
		}
		if err := runAutoFix(cfg, alias, r.Name); err != nil {
			results[i].Detail = fmt.Sprintf("%s (fix failed: %v)", r.Detail, err)
		}
	}
}

// runAutoFix dispatches to the appropriate fix function based on check name.
func runAutoFix(cfg *config.Config, alias, checkName string) error {
	switch checkName {
	case "Keychain entry exists":
		return fixMissingKeychain(cfg)
	case "Remote binary present":
		return fixMissingRemoteBinary(cfg, alias)
	case "Remote skill installed":
		return fixMissingSkill(cfg, alias)
	case "SSH RemoteForward configured":
		return fixMissingRemoteForward(cfg, alias)
	case "Local token file permissions":
		return fixTokenPerms(cfg)
	case "Remote token file permissions":
		return fixRemoteTokenPerms(cfg, alias)
	default:
		return fmt.Errorf("no auto-fix available for %q", checkName)
	}
}

// userHomeDir returns the user's home directory, or "." on error.
func userHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}
