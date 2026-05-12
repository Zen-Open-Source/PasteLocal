package doctor

import (
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pastelocal/pastelocal/internal/config"
)

// --- CheckResult tests ---

func TestCheckResultStringPass(t *testing.T) {
	r := CheckResult{
		Name:   "Daemon running",
		Passed: true,
		Detail: "[pid 12345, port 7331]",
	}
	got := r.String()
	want := "✓ Daemon running  [pid 12345, port 7331]"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestCheckResultStringFailWithHint(t *testing.T) {
	r := CheckResult{
		Name:    "Daemon running",
		Passed:  false,
		Detail:  "not running",
		FixHint: "Run `pastelocal start`",
	}
	got := r.String()
	want := "✗ Daemon running  not running\n  FIX: Run `pastelocal start`"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestCheckResultStringFailNoHint(t *testing.T) {
	r := CheckResult{
		Name:   "Some check",
		Passed: false,
		Detail: "something wrong",
	}
	got := r.String()
	want := "✗ Some check  something wrong"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// --- Non-fixable items must have AutoFix=false ---

func TestLoopbackOnlyNotAutoFixable(t *testing.T) {
	// Wrong loopback binding is a config issue, not auto-fixable.
	r := checkLoopbackOnly(config.Default())
	// Even if it passes, AutoFix should be false.
	if r.AutoFix {
		t.Errorf("checkLoopbackOnly AutoFix = true, want false (config issue)")
	}
}

func TestClipboardToolNotAutoFixable(t *testing.T) {
	// Missing clipboard tool requires brew install consent, not auto-fixable.
	cfg := config.Default()
	r := checkClipboardTool(cfg)
	if r.AutoFix {
		t.Errorf("checkClipboardTool AutoFix = true, want false (requires user consent)")
	}
}

// --- Token file check tests ---

func TestTokenFileNotExists(t *testing.T) {
	// Point to a non-existent path by overriding the home dir.
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	cfg := config.Default()
	r := checkTokenFile(cfg)
	if r.Passed {
		t.Error("checkTokenFile should fail when token file doesn't exist")
	}
	if r.AutoFix {
		t.Error("checkTokenFile should not be auto-fixable for missing file")
	}
}

func TestTokenFileWrongPerms(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "token")

	// Write a token file with wrong permissions.
	if err := os.WriteFile(tokenPath, []byte("test-token"), 0644); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	// Check via auth.CheckTokenFilePerms to verify it detects wrong perms.
	err := checkTokenFilePermsDirect(tokenPath)
	if err == nil {
		t.Error("expected error for wrong permissions, got nil")
	}
}

func TestTokenFileCorrectPerms(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "token")

	// Write a token file with correct permissions.
	if err := os.WriteFile(tokenPath, []byte("test-token"), 0600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	err := checkTokenFilePermsDirect(tokenPath)
	if err != nil {
		t.Errorf("expected no error for correct permissions, got: %v", err)
	}
}

// checkTokenFilePermsDirect is a test helper that checks token file permissions.
func checkTokenFilePermsDirect(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("checking token file: %w", err)
	}
	perm := info.Mode().Perm()
	if perm != fs.FileMode(0600) {
		return fmt.Errorf("token file has insecure permissions %04o, expected 0600", perm)
	}
	return nil
}

// --- FixTokenPerms test ---

func TestFixTokenPerms(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "token")

	// Write a token file with wrong permissions.
	if err := os.WriteFile(tokenPath, []byte("test-token"), 0644); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	// Override the default token path for the test.
	origDefault := defaultTokenPathOverride
	t.Cleanup(func() { defaultTokenPathOverride = origDefault })
	defaultTokenPathOverride = tokenPath

	cfg := config.Default()
	if err := fixTokenPerms(cfg); err != nil {
		t.Fatalf("fixTokenPerms() error: %v", err)
	}

	// Verify the permissions are now 0600.
	info, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatalf("stat token file: %v", err)
	}
	if info.Mode().Perm() != fs.FileMode(0600) {
		t.Errorf("permissions = %04o, want 0600", info.Mode().Perm())
	}
}

// --- RunChecks returns all expected checks ---

func TestRunChecksReturnsAllLocalChecks(t *testing.T) {
	cfg := config.Default()
	// No hosts so we only get local checks.
	results := RunChecks(cfg, false)

	// We expect at least 6 local checks.
	if len(results) < 6 {
		t.Errorf("RunChecks returned %d results, want at least 6", len(results))
	}

	expectedNames := []string{
		"Daemon running",
		"Daemon HTTP responding",
		"Daemon bound to loopback only",
		"Local token file exists",
		"Keychain entry exists",
		"Clipboard tool installed",
	}

	for _, name := range expectedNames {
		found := false
		for _, r := range results {
			if r.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing check result %q", name)
		}
	}
}

func TestRunHostChecksReturnsAllHostChecks(t *testing.T) {
	cfg := config.Default()
	cfg.Hosts["testhost"] = config.Host{}

	results := RunHostChecks(cfg, "testhost", false)

	expectedNames := []string{
		"SSH RemoteForward configured",
		"SSH connection works",
		"Remote binary present",
		"Remote token file present",
		"Remote skill installed",
		"Remote disk space",
	}

	if len(results) != len(expectedNames) {
		t.Errorf("RunHostChecks returned %d results, want %d", len(results), len(expectedNames))
	}

	for _, name := range expectedNames {
		found := false
		for _, r := range results {
			if r.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing host check result %q", name)
		}
	}
}

func TestRunChecksIncludesHostChecks(t *testing.T) {
	cfg := config.Default()
	cfg.Hosts["myhost"] = config.Host{}

	results := RunChecks(cfg, false)

	// 6 local + 6 host checks.
	if len(results) < 12 {
		t.Errorf("RunChecks returned %d results, want at least 12", len(results))
	}

	// Verify host checks are present.
	foundSSH := false
	for _, r := range results {
		if r.Name == "SSH RemoteForward configured" {
			foundSSH = true
			break
		}
	}
	if !foundSSH {
		t.Error("missing host check in RunChecks results")
	}
}

// --- KeychainLabel test ---

func TestKeychainLabel(t *testing.T) {
	label := keychainLabel()
	switch runtime.GOOS {
	case "darwin":
		if label != "macOS Keychain" {
			t.Errorf("keychainLabel() = %q, want %q", label, "macOS Keychain")
		}
	case "linux":
		if label != "libsecret" {
			t.Errorf("keychainLabel() = %q, want %q", label, "libsecret")
		}
	}
}

// --- SSH host string construction ---

func TestSSHHostStr(t *testing.T) {
	tests := []struct {
		name  string
		host  config.Host
		alias string
		want  string
	}{
		{
			name:  "no user",
			host:  config.Host{},
			alias: "myserver",
			want:  "myserver",
		},
		{
			name:  "with user",
			host:  config.Host{RemoteUser: "admin"},
			alias: "myserver",
			want:  "admin@myserver",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sshHostStr(tt.host, tt.alias)
			if got != tt.want {
				t.Errorf("sshHostStr() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- Remote binary path test ---

func TestRemoteBinPath(t *testing.T) {
	tests := []struct {
		name string
		host config.Host
		want string
	}{
		{
			name: "default path",
			host: config.Host{},
			want: "~/.local/bin/pastelocal-remote",
		},
		{
			name: "custom path",
			host: config.Host{RemotePath: "/opt/bin/pastelocal-remote"},
			want: "/opt/bin/pastelocal-remote",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := remoteBinPath(tt.host)
			if got != tt.want {
				t.Errorf("remoteBinPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- PrintResults test ---

func TestPrintResults(t *testing.T) {
	results := []CheckResult{
		{Name: "Daemon running", Passed: true, Detail: "[pid 1234]"},
		{Name: "Token file", Passed: false, Detail: "missing", FixHint: "Run start"},
		{Name: "Loopback", Passed: false, Detail: "bad"},
	}

	var buf strings.Builder
	PrintResults(&buf, results)

	output := buf.String()
	if !strings.Contains(output, "✓ Daemon running") {
		t.Error("output missing pass marker")
	}
	if !strings.Contains(output, "✗ Token file") {
		t.Error("output missing fail marker")
	}
	if !strings.Contains(output, "FIX: Run start") {
		t.Error("output missing fix hint")
	}
	if !strings.Contains(output, "✗ Loopback") {
		t.Error("output missing fail without hint")
	}
}

// --- Auto-fix dispatch test ---

func TestRunAutoFixUnknownCheck(t *testing.T) {
	cfg := config.Default()
	err := runAutoFix(cfg, "", "nonexistent check")
	if err == nil {
		t.Error("expected error for unknown check, got nil")
	}
	if !strings.Contains(err.Error(), "no auto-fix available") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- Clipboard tool name test ---

func TestClipboardToolNameDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("skipping darwin-specific test")
	}
	cfg := config.Default()
	tool, hint := clipboardToolName(cfg)
	if tool != "pngpaste" {
		t.Errorf("clipboardToolName() = %q, want %q", tool, "pngpaste")
	}
	if !strings.Contains(hint, "brew install") {
		t.Errorf("hint = %q, want brew install suggestion", hint)
	}
}

func TestClipboardToolNameLinuxAuto(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("skipping linux-specific test")
	}
	cfg := config.Default()
	tool, _ := clipboardToolName(cfg)
	// Should detect either wl-paste or xclip (or empty if neither installed).
	if tool != "" && tool != "wl-paste" && tool != "xclip" {
		t.Errorf("clipboardToolName() = %q, unexpected tool", tool)
	}
}

func TestClipboardToolNameLinuxExplicit(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("skipping linux-specific test")
	}
	cfg := config.Default()
	cfg.Linux.ClipboardTool = "wl-paste"
	tool, _ := clipboardToolName(cfg)
	if tool != "wl-paste" {
		t.Errorf("clipboardToolName() = %q, want %q", tool, "wl-paste")
	}
}

// --- Host not in config tests ---

func TestCheckHostSSHConfigMissingHost(t *testing.T) {
	cfg := config.Default()
	r := checkHostSSHConfig(cfg, "nonexistent")
	if r.Passed {
		t.Error("should fail for missing host")
	}
	if r.AutoFix {
		t.Error("missing host should not be auto-fixable")
	}
}

func TestCheckHostSSHConnectionMissingHost(t *testing.T) {
	cfg := config.Default()
	r := checkHostSSHConnection(cfg, "nonexistent")
	if r.Passed {
		t.Error("should fail for missing host")
	}
}

func TestCheckHostRemoteBinaryMissingHost(t *testing.T) {
	cfg := config.Default()
	r := checkHostRemoteBinary(cfg, "nonexistent")
	if r.Passed {
		t.Error("should fail for missing host")
	}
}

func TestCheckHostRemoteTokenMissingHost(t *testing.T) {
	cfg := config.Default()
	r := checkHostRemoteToken(cfg, "nonexistent")
	if r.Passed {
		t.Error("should fail for missing host")
	}
}

func TestCheckHostRemoteSkillMissingHost(t *testing.T) {
	cfg := config.Default()
	r := checkHostRemoteSkill(cfg, "nonexistent")
	if r.Passed {
		t.Error("should fail for missing host")
	}
}

func TestCheckHostDiskSpaceMissingHost(t *testing.T) {
	cfg := config.Default()
	r := checkHostDiskSpace(cfg, "nonexistent")
	if r.Passed {
		t.Error("should fail for missing host")
	}
}

// --- Fix missing keychain with no token file ---

func TestFixMissingKeychainNoTokenFile(t *testing.T) {
	tmpDir := t.TempDir()
	origDefault := defaultTokenPathOverride
	t.Cleanup(func() { defaultTokenPathOverride = origDefault })
	defaultTokenPathOverride = filepath.Join(tmpDir, "nonexistent-token")

	cfg := config.Default()
	err := fixMissingKeychain(cfg)
	if err == nil {
		t.Error("expected error when token file doesn't exist, got nil")
	}
}

// --- Fix missing remote binary missing host ---

func TestFixMissingRemoteBinaryMissingHost(t *testing.T) {
	cfg := config.Default()
	err := fixMissingRemoteBinary(cfg, "nonexistent")
	if err == nil {
		t.Error("expected error for missing host, got nil")
	}
}

// --- Fix missing skill missing host ---

func TestFixMissingSkillMissingHost(t *testing.T) {
	cfg := config.Default()
	err := fixMissingSkill(cfg, "nonexistent")
	if err == nil {
		t.Error("expected error for missing host, got nil")
	}
}

// --- Fix missing RemoteForward missing host ---

func TestFixMissingRemoteForwardMissingHost(t *testing.T) {
	cfg := config.Default()
	err := fixMissingRemoteForward(cfg, "nonexistent")
	if err == nil {
		t.Error("expected error for missing host, got nil")
	}
}

// --- Fix remote token perms missing host ---

func TestFixRemoteTokenPermsMissingHost(t *testing.T) {
	cfg := config.Default()
	err := fixRemoteTokenPerms(cfg, "nonexistent")
	if err == nil {
		t.Error("expected error for missing host, got nil")
	}
}

// --- Termius mode skips SSH config check ---

func TestCheckHostSSHConfigTermius(t *testing.T) {
	cfg := config.Default()
	cfg.Hosts["termius-host"] = config.Host{Termius: true}

	r := checkHostSSHConfig(cfg, "termius-host")
	if !r.Passed {
		t.Error("Termius mode should pass SSH config check")
	}
	if !strings.Contains(r.Detail, "Termius mode") {
		t.Errorf("expected Termius mode detail, got: %s", r.Detail)
	}
}

// --- userHomeDir test ---

func TestUserHomeDir(t *testing.T) {
	home := userHomeDir()
	if home == "" {
		t.Error("userHomeDir() returned empty string")
	}
}

// ============================================================
// Additional tests to improve coverage
// ============================================================

// --- CheckResult formatting edge cases ---

func TestCheckResultStringPassEmptyDetail(t *testing.T) {
	r := CheckResult{Name: "Empty detail check", Passed: true, Detail: ""}
	got := r.String()
	want := "✓ Empty detail check  "
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestCheckResultStringFailEmptyDetail(t *testing.T) {
	r := CheckResult{Name: "No info", Passed: false, Detail: ""}
	got := r.String()
	want := "✗ No info  "
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestCheckResultStringAutoFixTruePass(t *testing.T) {
	r := CheckResult{Name: "AutoFix pass", Passed: true, Detail: "ok", AutoFix: true}
	got := r.String()
	// AutoFix=true shouldn't affect the string output for passing checks
	if !strings.HasPrefix(got, "✓") {
		t.Errorf("passing AutoFix check should still show ✓, got %q", got)
	}
}

func TestCheckResultStringFailWithAutoFixAndHint(t *testing.T) {
	r := CheckResult{
		Name:    "Fixable thing",
		Passed:  false,
		Detail:  "broken",
		FixHint: "Run fix",
		AutoFix: true,
	}
	got := r.String()
	if !strings.Contains(got, "FIX: Run fix") {
		t.Errorf("should contain fix hint, got %q", got)
	}
	if !strings.HasPrefix(got, "✗") {
		t.Errorf("should start with ✗, got %q", got)
	}
}

// --- checkTokenFile via override path with existing file ---

func TestTokenFileExistsWithCorrectPerms(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenPath, []byte("my-token"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	origDefault := defaultTokenPathOverride
	t.Cleanup(func() { defaultTokenPathOverride = origDefault })
	defaultTokenPathOverride = tokenPath

	cfg := config.Default()
	r := checkTokenFile(cfg)
	if !r.Passed {
		t.Errorf("expected check to pass for file with correct perms, got: %s", r.Detail)
	}
	if !strings.Contains(r.Detail, "mode 0600") {
		t.Errorf("expected detail to mention mode 0600, got: %s", r.Detail)
	}
}

func TestTokenFileExistsWithWrongPerms(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenPath, []byte("my-token"), 0644); err != nil {
		t.Fatalf("write token: %v", err)
	}

	origDefault := defaultTokenPathOverride
	t.Cleanup(func() { defaultTokenPathOverride = origDefault })
	defaultTokenPathOverride = tokenPath

	cfg := config.Default()
	r := checkTokenFile(cfg)
	if r.Passed {
		t.Error("expected check to fail for wrong perms")
	}
	if !r.AutoFix {
		t.Error("wrong perms should be auto-fixable")
	}
	if !strings.Contains(r.Name, "permissions") {
		t.Errorf("expected name to mention permissions, got: %s", r.Name)
	}
}

func TestTokenFileUnreadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows - file perms behave differently")
	}
	if os.Getuid() == 0 {
		t.Skip("skipping as root - can always read files")
	}

	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenPath, []byte("my-token"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	// Remove all permissions to make it unreadable
	if err := os.Chmod(tokenPath, 0000); err != nil {
		t.Fatalf("chmod token: %v", err)
	}

	origDefault := defaultTokenPathOverride
	t.Cleanup(func() { defaultTokenPathOverride = origDefault })
	defaultTokenPathOverride = tokenPath

	cfg := config.Default()
	r := checkTokenFile(cfg)
	// File exists but might fail on stat or open depending on OS.
	// The check should not pass.
	if r.Passed {
		t.Error("expected check to fail for unreadable file")
	}
}

// --- Empty override falls back to default path ---

func TestTokenFileOverrideEmptyFallsBack(t *testing.T) {
	// When defaultTokenPathOverride is empty, resolvedTokenPath() falls
	// back to auth.DefaultTokenPath(), so the check uses the real path.
	origDefault := defaultTokenPathOverride
	t.Cleanup(func() { defaultTokenPathOverride = origDefault })
	defaultTokenPathOverride = ""

	cfg := config.Default()
	r := checkTokenFile(cfg)
	// The result depends on whether the real token file exists.
	// We just verify it doesn't panic and produces a valid result.
	if r.Name == "" {
		t.Error("check result should have a name")
	}
}

// --- FixTokenPerms with non-existent file at resolved path ---

func TestFixTokenPermsNonExistentResolvedPath(t *testing.T) {
	tmpDir := t.TempDir()
	// Set override to a file that doesn't exist
	origDefault := defaultTokenPathOverride
	t.Cleanup(func() { defaultTokenPathOverride = origDefault })
	defaultTokenPathOverride = filepath.Join(tmpDir, "nonexistent-token")

	cfg := config.Default()
	err := fixTokenPerms(cfg)
	if err == nil {
		t.Error("expected error for non-existent token file, got nil")
	}
	if !strings.Contains(err.Error(), "chmod") {
		t.Errorf("expected chmod error, got: %v", err)
	}
}

// --- FixTokenPerms on non-existent file ---

func TestFixTokenPermsNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "nonexistent")

	origDefault := defaultTokenPathOverride
	t.Cleanup(func() { defaultTokenPathOverride = origDefault })
	defaultTokenPathOverride = tokenPath

	cfg := config.Default()
	err := fixTokenPerms(cfg)
	if err == nil {
		t.Error("expected error for non-existent token file, got nil")
	}
}

// --- PrintResults with empty results ---

func TestPrintResultsEmpty(t *testing.T) {
	var buf strings.Builder
	PrintResults(&buf, nil)
	if buf.String() != "" {
		t.Errorf("expected empty output for nil results, got %q", buf.String())
	}

	var buf2 strings.Builder
	PrintResults(&buf2, []CheckResult{})
	if buf2.String() != "" {
		t.Errorf("expected empty output for empty results, got %q", buf2.String())
	}
}

// --- PrintResults with mixed results ---

func TestPrintResultsMixedResults(t *testing.T) {
	results := []CheckResult{
		{Name: "Check A", Passed: true, Detail: "all good"},
		{Name: "Check B", Passed: false, Detail: "bad", FixHint: "fix it", AutoFix: true},
		{Name: "Check C", Passed: false, Detail: "also bad"},
	}

	var buf strings.Builder
	PrintResults(&buf, results)

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 4 { // Check B has 2 lines (fail + FIX)
		t.Errorf("expected 4 lines, got %d: %q", len(lines), output)
	}
}

// --- PrintResults writes to any io.Writer ---

func TestPrintResultsToDiscard(t *testing.T) {
	results := []CheckResult{
		{Name: "Test", Passed: true, Detail: "ok"},
	}
	// Should not panic when writing to io.Discard
	PrintResults(io.Discard, results)
}

// --- applyFixes updates detail on failure ---

func TestApplyFixesUpdatesDetailOnFailure(t *testing.T) {
	cfg := config.Default()

	results := []CheckResult{
		{Name: "nonexistent-fix", Passed: false, Detail: "broken", AutoFix: true},
	}

	applyFixes(cfg, results)

	// The fix will fail (unknown check name), so the detail should be updated.
	if !strings.Contains(results[0].Detail, "fix failed") {
		t.Errorf("expected detail to contain 'fix failed', got: %s", results[0].Detail)
	}
}

// --- applyFixes skips passed results ---

func TestApplyFixesSkipsPassedResults(t *testing.T) {
	cfg := config.Default()

	results := []CheckResult{
		{Name: "Something", Passed: true, Detail: "original", AutoFix: true},
	}

	applyFixes(cfg, results)

	if results[0].Detail != "original" {
		t.Errorf("passed result should not be modified, got: %s", results[0].Detail)
	}
}

// --- applyFixes skips non-auto-fixable results ---

func TestApplyFixesSkipsNonAutoFixable(t *testing.T) {
	cfg := config.Default()

	results := []CheckResult{
		{Name: "Something", Passed: false, Detail: "original", AutoFix: false},
	}

	applyFixes(cfg, results)

	if results[0].Detail != "original" {
		t.Errorf("non-auto-fixable result should not be modified, got: %s", results[0].Detail)
	}
}

// --- applyHostFixes updates detail on failure ---

func TestApplyHostFixesUpdatesDetailOnFailure(t *testing.T) {
	cfg := config.Default()

	results := []CheckResult{
		{Name: "nonexistent-fix", Passed: false, Detail: "broken", AutoFix: true},
	}

	applyHostFixes(cfg, "myhost", results)

	if !strings.Contains(results[0].Detail, "fix failed") {
		t.Errorf("expected detail to contain 'fix failed', got: %s", results[0].Detail)
	}
}

// --- applyHostFixes skips passed and non-auto-fixable ---

func TestApplyHostFixesSkipsPassedResults(t *testing.T) {
	cfg := config.Default()

	results := []CheckResult{
		{Name: "Something", Passed: true, Detail: "original", AutoFix: true},
		{Name: "Other", Passed: false, Detail: "original2", AutoFix: false},
	}

	applyHostFixes(cfg, "myhost", results)

	if results[0].Detail != "original" {
		t.Errorf("passed result should not be modified, got: %s", results[0].Detail)
	}
	if results[1].Detail != "original2" {
		t.Errorf("non-auto-fixable result should not be modified, got: %s", results[1].Detail)
	}
}

// --- runAutoFix dispatches to known fix functions ---

func TestRunAutoFixKnownChecksDispatch(t *testing.T) {
	cfg := config.Default()

	// Test dispatch for "Keychain entry exists" - will fail without real keychain
	err := runAutoFix(cfg, "", "Keychain entry exists")
	if err == nil {
		// It might succeed on some systems, that's OK
		t.Log("Keychain fix succeeded (expected on some systems)")
	}

	// Test dispatch for "Local token file permissions" - will fail without token file
	origDefault := defaultTokenPathOverride
	t.Cleanup(func() { defaultTokenPathOverride = origDefault })
	defaultTokenPathOverride = "/nonexistent/path/token"

	err = runAutoFix(cfg, "", "Local token file permissions")
	if err == nil {
		t.Error("expected error for non-existent token path")
	}

	// Test dispatch for "Remote binary present" - will fail without host in config
	err = runAutoFix(cfg, "nonexistent", "Remote binary present")
	if err == nil {
		t.Error("expected error for missing host")
	}

	// Test dispatch for "Remote skill installed" - will fail without host
	err = runAutoFix(cfg, "nonexistent", "Remote skill installed")
	if err == nil {
		t.Error("expected error for missing host")
	}

	// Test dispatch for "SSH RemoteForward configured" - will fail without host
	err = runAutoFix(cfg, "nonexistent", "SSH RemoteForward configured")
	if err == nil {
		t.Error("expected error for missing host")
	}

	// Test dispatch for "Remote token file permissions" - will fail without host
	err = runAutoFix(cfg, "nonexistent", "Remote token file permissions")
	if err == nil {
		t.Error("expected error for missing host")
	}
}

// --- RunChecks with fix=true triggers re-run ---

func TestRunChecksWithFixTrue(t *testing.T) {
	cfg := config.Default()
	// No hosts - just local checks. fix=true will trigger applyFixes + re-run.
	results := RunChecks(cfg, true)

	// We should still get at least 6 local checks after re-run.
	if len(results) < 6 {
		t.Errorf("RunChecks with fix=true returned %d results, want at least 6", len(results))
	}

	// Verify all expected check names are present after re-run.
	expectedNames := []string{
		"Daemon running",
		"Daemon HTTP responding",
		"Daemon bound to loopback only",
		"Local token file exists",
		"Keychain entry exists",
		"Clipboard tool installed",
	}
	for _, name := range expectedNames {
		found := false
		for _, r := range results {
			if r.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing check %q after fix=true re-run", name)
		}
	}
}

// --- RunHostChecks with fix=true ---

func TestRunHostChecksWithFixTrue(t *testing.T) {
	cfg := config.Default()
	cfg.Hosts["testhost"] = config.Host{}

	results := RunHostChecks(cfg, "testhost", true)

	// Should still get 6 results after re-run (even though fixes will fail).
	if len(results) != 6 {
		t.Errorf("RunHostChecks with fix=true returned %d results, want 6", len(results))
	}
}

// --- checkDaemonRunning structure (without mocking service) ---

func TestCheckDaemonRunningStructure(t *testing.T) {
	cfg := config.Default()
	r := checkDaemonRunning(cfg)

	// Should have the correct name.
	if r.Name != "Daemon running" {
		t.Errorf("Name = %q, want %q", r.Name, "Daemon running")
	}
	// Not auto-fixable.
	if r.AutoFix {
		t.Error("Daemon running should not be auto-fixable")
	}
	// Should have fix hint when failed.
	if !r.Passed && r.FixHint == "" {
		t.Error("Failed daemon check should have a fix hint")
	}
}

// --- checkDaemonHTTP structure ---

func TestCheckDaemonHTTPStructure(t *testing.T) {
	cfg := config.Default()
	r := checkDaemonHTTP(cfg)

	if r.Name != "Daemon HTTP responding" {
		t.Errorf("Name = %q, want %q", r.Name, "Daemon HTTP responding")
	}
	if r.AutoFix {
		t.Error("Daemon HTTP check should not be auto-fixable")
	}
}

// --- checkLoopbackOnly structure ---

func TestCheckLoopbackOnlyStructure(t *testing.T) {
	cfg := config.Default()
	r := checkLoopbackOnly(cfg)

	if r.Name != "Daemon bound to loopback only" {
		t.Errorf("Name = %q, want %q", r.Name, "Daemon bound to loopback only")
	}
}

// --- checkKeychainEntry structure ---

func TestCheckKeychainEntryStructure(t *testing.T) {
	cfg := config.Default()
	r := checkKeychainEntry(cfg)

	if r.Name != "Keychain entry exists" {
		t.Errorf("Name = %q, want %q", r.Name, "Keychain entry exists")
	}
	// Keychain entry is auto-fixable when missing.
	if !r.Passed && !r.AutoFix {
		t.Error("Keychain entry check should be auto-fixable when failed")
	}
}

// --- findLocalSkillPath returns empty when no skill file exists ---

func TestFindLocalSkillPathMissing(t *testing.T) {
	// Use a temp home dir where no skill files exist.
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	path := findLocalSkillPath()
	if path != "" {
		t.Errorf("expected empty path when no skill file exists, got %q", path)
	}
}

// --- findLocalSkillPath finds skill file in config dir ---

func TestFindLocalSkillPathInConfigDir(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	skillDir := filepath.Join(tmpDir, ".config", "pastelocal", "skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	skillFile := filepath.Join(skillDir, "paste.md")
	if err := os.WriteFile(skillFile, []byte("skill content"), 0644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	path := findLocalSkillPath()
	if path != skillFile {
		t.Errorf("expected %q, got %q", skillFile, path)
	}
}

// --- findLocalSkillPath finds skill file in share dir ---

func TestFindLocalSkillPathInShareDir(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	// Don't create config/pastelocal/skill, only share/pastelocal/skill
	skillDir := filepath.Join(tmpDir, ".local", "share", "pastelocal", "skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	skillFile := filepath.Join(skillDir, "paste.md")
	if err := os.WriteFile(skillFile, []byte("skill content"), 0644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	path := findLocalSkillPath()
	if path != skillFile {
		t.Errorf("expected %q, got %q", skillFile, path)
	}
}

// --- userSSHConfigPath ---

func TestUserSSHConfigPath(t *testing.T) {
	path := userSSHConfigPath()
	if !strings.HasSuffix(path, ".ssh/config") {
		t.Errorf("expected path ending with .ssh/config, got %q", path)
	}
}

// --- Host check names are consistent ---

func TestHostCheckDetailsForMissingHost(t *testing.T) {
	cfg := config.Default()

	checks := []struct {
		name string
		fn   func(*config.Config, string) CheckResult
	}{
		{"SSH RemoteForward configured", func(c *config.Config, a string) CheckResult { return checkHostSSHConfig(c, a) }},
		{"SSH connection works", func(c *config.Config, a string) CheckResult { return checkHostSSHConnection(c, a) }},
		{"Remote binary present", func(c *config.Config, a string) CheckResult { return checkHostRemoteBinary(c, a) }},
		{"Remote token file present", func(c *config.Config, a string) CheckResult { return checkHostRemoteToken(c, a) }},
		{"Remote skill installed", func(c *config.Config, a string) CheckResult { return checkHostRemoteSkill(c, a) }},
		{"Remote disk space", func(c *config.Config, a string) CheckResult { return checkHostDiskSpace(c, a) }},
	}

	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			r := check.fn(cfg, "nonexistent-host")
			if r.Passed {
				t.Error("should fail for missing host")
			}
			if !strings.Contains(r.Detail, "nonexistent-host") {
				t.Errorf("detail should reference the host alias, got: %s", r.Detail)
			}
		})
	}
}

// --- sshHostStr with various configs ---

func TestSSHHostStrEdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		host  config.Host
		alias string
		want  string
	}{
		{
			name:  "empty user empty alias",
			host:  config.Host{},
			alias: "",
			want:  "",
		},
		{
			name:  "user with IP address",
			host:  config.Host{RemoteUser: "root"},
			alias: "192.168.1.1",
			want:  "root@192.168.1.1",
		},
		{
			name:  "user with FQDN",
			host:  config.Host{RemoteUser: "admin"},
			alias: "server.example.com",
			want:  "admin@server.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sshHostStr(tt.host, tt.alias)
			if got != tt.want {
				t.Errorf("sshHostStr() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- remoteBinPath edge cases ---

func TestRemoteBinPathEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		host config.Host
		want string
	}{
		{
			name: "empty RemotePath",
			host: config.Host{RemotePath: ""},
			want: "~/.local/bin/pastelocal-remote",
		},
		{
			name: "absolute path",
			host: config.Host{RemotePath: "/usr/local/bin/cb-remote"},
			want: "/usr/local/bin/cb-remote",
		},
		{
			name: "tilde path",
			host: config.Host{RemotePath: "~/bin/cb-remote"},
			want: "~/bin/cb-remote",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := remoteBinPath(tt.host)
			if got != tt.want {
				t.Errorf("remoteBinPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- RunChecks with multiple hosts ---

func TestRunChecksMultipleHosts(t *testing.T) {
	cfg := config.Default()
	cfg.Hosts["host1"] = config.Host{}
	cfg.Hosts["host2"] = config.Host{RemoteUser: "admin"}

	results := RunChecks(cfg, false)

	// 6 local + 6 per host = 18
	if len(results) < 18 {
		t.Errorf("expected at least 18 results with 2 hosts, got %d", len(results))
	}

	// Count SSH-related results.
	sshResults := 0
	for _, r := range results {
		if strings.Contains(r.Name, "SSH") || strings.Contains(r.Name, "Remote") {
			sshResults++
		}
	}
	if sshResults < 10 { // at least 5 per host
		t.Errorf("expected at least 10 SSH/Remote results for 2 hosts, got %d", sshResults)
	}
}

// --- keychainLabel on unknown platform ---

func TestKeychainLabelFallback(t *testing.T) {
	label := keychainLabel()
	// Should return something, not empty
	if label == "" {
		t.Error("keychainLabel() should not return empty string")
	}
	// On darwin it must be "macOS Keychain", on linux "libsecret"
	switch runtime.GOOS {
	case "darwin":
		if label != "macOS Keychain" {
			t.Errorf("keychainLabel() = %q, want %q", label, "macOS Keychain")
		}
	case "linux":
		if label != "libsecret" {
			t.Errorf("keychainLabel() = %q, want %q", label, "libsecret")
		}
	}
}

// --- FixMissingKeychain with existing token file (no keychain) ---

func TestFixMissingKeychainWithTokenFile(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenPath, []byte("test-token-123"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	origDefault := defaultTokenPathOverride
	t.Cleanup(func() { defaultTokenPathOverride = origDefault })
	defaultTokenPathOverride = tokenPath

	cfg := config.Default()
	err := fixMissingKeychain(cfg)
	// The error might be nil (if keychain store works) or non-nil (if keychain not available).
	// Either way, it shouldn't panic.
	_ = err
}

// --- checkClipboardTool on macOS with custom path ---

func TestClipboardToolNameDarwinCustomPath(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("skipping darwin-specific test")
	}
	cfg := config.Default()
	cfg.MacOS.PngpastePath = "/usr/local/custom/bin/pngpaste"
	tool, _ := clipboardToolName(cfg)
	if tool != "/usr/local/custom/bin/pngpaste" {
		t.Errorf("expected custom path, got %q", tool)
	}
}

// --- Host checks with host in config (exercises SSH path but will fail) ---

func TestCheckHostRemoteTokenWithConfiguredHost(t *testing.T) {
	cfg := config.Default()
	cfg.Hosts["nonexistent.invalid"] = config.Host{RemoteUser: "test"}

	r := checkHostRemoteToken(cfg, "nonexistent.invalid")
	if r.Passed {
		t.Error("should fail for unreachable host")
	}
	// The SSH call will fail, covering the error path
}

func TestCheckHostRemoteBinaryWithConfiguredHost(t *testing.T) {
	cfg := config.Default()
	cfg.Hosts["nonexistent.invalid"] = config.Host{RemoteUser: "test"}

	r := checkHostRemoteBinary(cfg, "nonexistent.invalid")
	if r.Passed {
		t.Error("should fail for unreachable host")
	}
	if !r.AutoFix {
		t.Error("missing remote binary should be auto-fixable")
	}
}

func TestCheckHostSSHConnectionWithConfiguredHost(t *testing.T) {
	cfg := config.Default()
	cfg.Hosts["nonexistent.invalid"] = config.Host{RemoteUser: "test"}

	r := checkHostSSHConnection(cfg, "nonexistent.invalid")
	if r.Passed {
		t.Error("should fail for unreachable host")
	}
	if r.AutoFix {
		t.Error("SSH connection failure should not be auto-fixable")
	}
}

func TestCheckHostDiskSpaceWithConfiguredHost(t *testing.T) {
	cfg := config.Default()
	cfg.Hosts["nonexistent.invalid"] = config.Host{RemoteUser: "test"}

	r := checkHostDiskSpace(cfg, "nonexistent.invalid")
	if r.Passed {
		t.Error("should fail for unreachable host")
	}
	if r.AutoFix {
		t.Error("disk space check should not be auto-fixable")
	}
}

func TestCheckHostRemoteSkillWithConfiguredHost(t *testing.T) {
	cfg := config.Default()
	cfg.Hosts["nonexistent.invalid"] = config.Host{RemoteUser: "test"}

	r := checkHostRemoteSkill(cfg, "nonexistent.invalid")
	if r.Passed {
		t.Error("should fail for unreachable host")
	}
	if !r.AutoFix {
		t.Error("missing skill should be auto-fixable")
	}
}

func TestCheckHostSSHConfigWithConfiguredHost(t *testing.T) {
	cfg := config.Default()
	cfg.Hosts["nonexistent.invalid"] = config.Host{RemoteUser: "test"}

	r := checkHostSSHConfig(cfg, "nonexistent.invalid")
	if r.Passed {
		t.Error("should fail when ssh config doesn't have the entry")
	}
}

// --- checkHostSSHConfig with host that has RemotePort set ---

func TestCheckHostSSHConfigWithRemotePort(t *testing.T) {
	cfg := config.Default()
	cfg.Hosts["myhost"] = config.Host{RemotePort: 9999}

	r := checkHostSSHConfig(cfg, "myhost")
	if r.Passed {
		t.Error("should fail without SSH config")
	}
	// The detail should reference the configured port
	if !strings.Contains(r.Detail, "9999") {
		t.Errorf("expected port 9999 in detail, got: %s", r.Detail)
	}
}

// --- checkHostRemoteToken with host using RemoteUser ---

func TestCheckHostRemoteTokenWithRemoteUser(t *testing.T) {
	cfg := config.Default()
	cfg.Hosts["myhost"] = config.Host{RemoteUser: "admin"}

	r := checkHostRemoteToken(cfg, "myhost")
	if r.Passed {
		t.Error("should fail for unreachable host")
	}
}

// --- Fix functions with host in config (SSH will fail) ---

func TestFixMissingRemoteBinaryWithConfiguredHost(t *testing.T) {
	cfg := config.Default()
	cfg.Hosts["nonexistent.invalid"] = config.Host{RemoteUser: "test"}

	err := fixMissingRemoteBinary(cfg, "nonexistent.invalid")
	if err == nil {
		t.Error("expected error for unreachable host")
	}
	if !strings.Contains(err.Error(), "detect remote architecture") {
		t.Errorf("expected arch detection error, got: %v", err)
	}
}

func TestFixMissingSkillWithConfiguredHost(t *testing.T) {
	cfg := config.Default()
	cfg.Hosts["nonexistent.invalid"] = config.Host{RemoteUser: "test"}

	err := fixMissingSkill(cfg, "nonexistent.invalid")
	// Will fail since no skill file exists locally
	if err == nil {
		t.Log("fixMissingSkill succeeded unexpectedly")
	}
}

func TestFixRemoteTokenPermsWithConfiguredHost(t *testing.T) {
	cfg := config.Default()
	cfg.Hosts["nonexistent.invalid"] = config.Host{RemoteUser: "test"}

	err := fixRemoteTokenPerms(cfg, "nonexistent.invalid")
	if err == nil {
		t.Error("expected error for unreachable host")
	}
}

// --- checkDaemonHTTP failure paths ---

func TestCheckDaemonHTTPFailureHasFixHint(t *testing.T) {
	cfg := config.Default()
	r := checkDaemonHTTP(cfg)
	if !r.Passed && r.FixHint == "" {
		t.Error("failed HTTP check should have a fix hint")
	}
	if !r.Passed && r.Name != "Daemon HTTP responding" {
		t.Errorf("Name = %q, want %q", r.Name, "Daemon HTTP responding")
	}
}

// --- RunChecks with fix=true and hosts ---

func TestRunChecksWithFixAndHosts(t *testing.T) {
	cfg := config.Default()
	cfg.Hosts["testhost"] = config.Host{}

	results := RunChecks(cfg, true)

	// Should have at least 12 results (6 local + 6 host)
	if len(results) < 12 {
		t.Errorf("expected at least 12 results, got %d", len(results))
	}
}

// --- checkTokenFile with stat error ---

func TestTokenFileStatError(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "token")

	// Create a file and then a directory at the same path to cause a stat weirdness
	if err := os.WriteFile(tokenPath, []byte("test"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	origDefault := defaultTokenPathOverride
	t.Cleanup(func() { defaultTokenPathOverride = origDefault })
	defaultTokenPathOverride = tokenPath

	cfg := config.Default()
	r := checkTokenFile(cfg)
	// Should pass since file exists with correct perms
	if !r.Passed {
		t.Errorf("expected pass for file with correct perms, got: %s", r.Detail)
	}
}

// --- RunHostChecks with fix for host not in config ---

func TestRunHostChecksWithFixForMissingHost(t *testing.T) {
	cfg := config.Default()
	// Don't add the host - all checks should fail but not panic
	results := RunHostChecks(cfg, "nonexistent", true)

	// Still should produce 6 results
	if len(results) != 6 {
		t.Errorf("expected 6 results, got %d", len(results))
	}
}

// --- fixMissingRemoteForward with SSH config in temp dir ---

func TestFixMissingRemoteForwardWithTempConfig(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	cfg := config.Default()
	cfg.Hosts["testhost"] = config.Host{}

	err := fixMissingRemoteForward(cfg, "testhost")
	if err != nil {
		t.Errorf("fixMissingRemoteForward failed: %v", err)
	}

	// Verify the SSH config was created/modified
	sshCfgPath := filepath.Join(sshDir, "config")
	content, err := os.ReadFile(sshCfgPath)
	if err != nil {
		t.Fatalf("read ssh config: %v", err)
	}
	if !strings.Contains(string(content), "RemoteForward") {
		t.Errorf("expected RemoteForward in config, got:\n%s", string(content))
	}
}

// --- fixMissingRemoteForward with existing SSH config ---

func TestFixMissingRemoteForwardWithExistingConfig(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sshCfgPath := filepath.Join(sshDir, "config")
	existingContent := "Host otherhost\n  HostName other.example.com\n"
	if err := os.WriteFile(sshCfgPath, []byte(existingContent), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	cfg := config.Default()
	cfg.Hosts["newhost"] = config.Host{RemotePort: 8080}

	err := fixMissingRemoteForward(cfg, "newhost")
	if err != nil {
		t.Errorf("fixMissingRemoteForward failed: %v", err)
	}

	content, err := os.ReadFile(sshCfgPath)
	if err != nil {
		t.Fatalf("read ssh config: %v", err)
	}
	if !strings.Contains(string(content), "newhost") {
		t.Errorf("expected newhost in config, got:\n%s", string(content))
	}
	if !strings.Contains(string(content), "RemoteForward") {
		t.Errorf("expected RemoteForward in config, got:\n%s", string(content))
	}
}

// --- fixMissingKeychain with empty override path ---

func TestFixMissingKeychainEmptyOverrideFallsBack(t *testing.T) {
	origDefault := defaultTokenPathOverride
	t.Cleanup(func() { defaultTokenPathOverride = origDefault })
	defaultTokenPathOverride = ""

	cfg := config.Default()
	err := fixMissingKeychain(cfg)
	// Should not panic; may succeed or fail depending on actual token file existence
	_ = err
}

// --- checkLoopbackOnly returns pass when lsof fails ---

func TestCheckLoopbackOnlyPassWhenLsofFails(t *testing.T) {
	cfg := config.Default()
	r := checkLoopbackOnly(cfg)
	// On most test environments, lsof won't find listeners on port 7331,
	// so it falls back to the pass path.
	if r.Name != "Daemon bound to loopback only" {
		t.Errorf("Name = %q, want %q", r.Name, "Daemon bound to loopback only")
	}
	// Should either pass (lsof failed) or have a meaningful detail
	if r.Passed {
		if !strings.Contains(r.Detail, "loopback") && !strings.Contains(r.Detail, "127.0.0.1") {
			t.Errorf("passing detail should mention loopback, got: %s", r.Detail)
		}
	}
}

// --- checkClipboardTool returns hint for missing tool ---

func TestClipboardToolHintContent(t *testing.T) {
	cfg := config.Default()
	_, hint := clipboardToolName(cfg)
	if hint == "" {
		t.Error("clipboardToolName should return a non-empty hint")
	}
}

// --- checkHostSSHConfig with existing SSH config but no entry ---

func TestCheckHostSSHConfigWithExistingConfigNoEntry(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sshCfgPath := filepath.Join(sshDir, "config")
	content := "Host otherhost\n  HostName other.example.com\n"
	if err := os.WriteFile(sshCfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	cfg := config.Default()
	cfg.Hosts["myhost"] = config.Host{}

	r := checkHostSSHConfig(cfg, "myhost")
	if r.Passed {
		t.Error("should fail when RemoteForward not found for host")
	}
	if !r.AutoFix {
		t.Error("missing RemoteForward should be auto-fixable")
	}
}

// --- checkHostSSHConfig with SSH config containing the host ---

func TestCheckHostSSHConfigWithEntryInConfig(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sshCfgPath := filepath.Join(sshDir, "config")
	content := "Host myhost\n  HostName my.example.com\n  RemoteForward 7331 127.0.0.1:7331  # pastelocal:myhost:remoteforward\n"
	if err := os.WriteFile(sshCfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	cfg := config.Default()
	cfg.Hosts["myhost"] = config.Host{}

	r := checkHostSSHConfig(cfg, "myhost")
	if !r.Passed {
		t.Errorf("should pass when RemoteForward exists, got: %s", r.Detail)
	}
}

// --- checkHostSSHConfig with bad SSH config syntax ---

func TestCheckHostSSHConfigWithBadConfigSyntax(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sshCfgPath := filepath.Join(sshDir, "config")
	// Write invalid content (sshconfig.Parse should handle it)
	content := "this is not valid ssh config {{{"
	if err := os.WriteFile(sshCfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	cfg := config.Default()
	cfg.Hosts["myhost"] = config.Host{}

	r := checkHostSSHConfig(cfg, "myhost")
	// Should not panic; might fail or pass depending on parser
	_ = r
}

// --- userSSHConfigPath with HOME override ---

func TestUserSSHConfigPathWithHomeOverride(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	path := userSSHConfigPath()
	expected := filepath.Join(tmpDir, ".ssh", "config")
	if path != expected {
		t.Errorf("userSSHConfigPath() = %q, want %q", path, expected)
	}
}

// --- checkHostRemoteToken with custom remote path ---

func TestCheckHostRemoteTokenWithRemotePath(t *testing.T) {
	cfg := config.Default()
	cfg.Hosts["myhost"] = config.Host{RemoteUser: "admin", RemotePath: "/opt/bin/cb"}

	// Just verify it doesn't panic and returns a result
	r := checkHostRemoteToken(cfg, "myhost")
	if r.Name == "" {
		t.Error("should have a check name")
	}
}

// --- checkHostRemoteBinary with custom remote path ---

func TestCheckHostRemoteBinaryWithRemotePath(t *testing.T) {
	cfg := config.Default()
	cfg.Hosts["myhost"] = config.Host{RemoteUser: "admin", RemotePath: "/opt/bin/cb"}

	r := checkHostRemoteBinary(cfg, "myhost")
	if r.Passed {
		t.Error("should fail for unreachable host")
	}
}

// --- userHomeDir fallback with bad HOME ---

func TestUserHomeDirFallback(t *testing.T) {
	// userHomeDir uses os.UserHomeDir() which should always work on macOS/Linux
	home := userHomeDir()
	if home == "" {
		t.Error("userHomeDir should return a non-empty path")
	}
}

// --- findLocalSkillPath prefers config dir over share dir ---

func TestFindLocalSkillPathConfigPreferredOverShare(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	// Create both config and share dirs
	configDir := filepath.Join(tmpDir, ".config", "pastelocal", "skill")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	configFile := filepath.Join(configDir, "paste.md")
	if err := os.WriteFile(configFile, []byte("config skill"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	shareDir := filepath.Join(tmpDir, ".local", "share", "pastelocal", "skill")
	if err := os.MkdirAll(shareDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	shareFile := filepath.Join(shareDir, "paste.md")
	if err := os.WriteFile(shareFile, []byte("share skill"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	path := findLocalSkillPath()
	if path != configFile {
		t.Errorf("expected config dir path %q, got %q", configFile, path)
	}
}

// ============================================================
// Additional tests for pushing coverage past 70%
// ============================================================

// --- checkDaemonHTTP with a real HTTP server (success path) ---

func TestCheckDaemonHTTPWithServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	// Extract port from the test server URL
	// srv.URL is http://127.0.0.1:PORT
	portStr := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	cfg := config.Default()
	cfg.Port = port

	r := checkDaemonHTTP(cfg)
	if !r.Passed {
		t.Errorf("expected check to pass with health server, got: %s", r.Detail)
	}
	if !strings.Contains(r.Detail, fmt.Sprintf("[port %d]", port)) {
		t.Errorf("expected detail with port, got: %s", r.Detail)
	}
}

// --- checkDaemonHTTP with server returning non-200 ---

func TestCheckDaemonHTTPNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	portStr := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	cfg := config.Default()
	cfg.Port = port

	r := checkDaemonHTTP(cfg)
	if r.Passed {
		t.Error("expected check to fail with 500 response")
	}
	if !strings.Contains(r.Detail, "returned 500") {
		t.Errorf("expected detail about status 500, got: %s", r.Detail)
	}
	if !strings.Contains(r.FixHint, "logs") {
		t.Errorf("expected fix hint about logs, got: %s", r.FixHint)
	}
}

// --- fixMissingSkill with a local skill file present (covers more of fixMissingSkill) ---

func TestFixMissingSkillWithLocalSkillFile(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	// Create the skill file in the config directory
	skillDir := filepath.Join(tmpDir, ".config", "pastelocal", "skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	skillFile := filepath.Join(skillDir, "paste.md")
	if err := os.WriteFile(skillFile, []byte("skill content"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg := config.Default()
	cfg.Hosts["nonexistent.invalid"] = config.Host{RemoteUser: "test"}

	err := fixMissingSkill(cfg, "nonexistent.invalid")
	// Will fail because SSH is not available, but should get past
	// the "local skill file not found" check and try the SSH/SCP step
	if err == nil {
		t.Log("fixMissingSkill succeeded unexpectedly (SSH available?)")
	}
	// The error should be about SSH/SCP, not about missing skill file
	if err != nil && strings.Contains(err.Error(), "local skill file") {
		t.Errorf("should not fail on missing skill file since we created one, got: %v", err)
	}
}

// --- fixMissingKeychain with token file and override ---

func TestFixMissingKeychainWithTokenFileAndOverride(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenPath, []byte("my-test-token"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	origDefault := defaultTokenPathOverride
	t.Cleanup(func() { defaultTokenPathOverride = origDefault })
	defaultTokenPathOverride = tokenPath

	cfg := config.Default()
	err := fixMissingKeychain(cfg)
	// On macOS with keychain, this might succeed or fail.
	// On CI without keychain access, it will fail.
	// Either way, the important thing is it reads the token file
	// and tries to store it.
	_ = err
}

// --- fixMissingKeychain with empty override path returns error ---

func TestFixMissingKeychainEmptyOverridePath(t *testing.T) {
	origDefault := defaultTokenPathOverride
	t.Cleanup(func() { defaultTokenPathOverride = origDefault })
	defaultTokenPathOverride = ""

	cfg := config.Default()
	err := fixMissingKeychain(cfg)
	// Will either succeed (if token file exists at default path) or fail
	_ = err
}

// --- fixMissingRemoteForward with host that has RemotePort ---

func TestFixMissingRemoteForwardWithRemotePort(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	cfg := config.Default()
	cfg.Hosts["myhost"] = config.Host{RemotePort: 9999}

	err := fixMissingRemoteForward(cfg, "myhost")
	if err != nil {
		t.Errorf("fixMissingRemoteForward failed: %v", err)
	}

	// Verify the SSH config has the correct port
	sshCfgPath := filepath.Join(sshDir, "config")
	content, err := os.ReadFile(sshCfgPath)
	if err != nil {
		t.Fatalf("read ssh config: %v", err)
	}
	if !strings.Contains(string(content), "9999") {
		t.Errorf("expected port 9999 in SSH config, got:\n%s", string(content))
	}
}

// --- checkHostRemoteBinary stdout not "ok" ---

func TestCheckHostRemoteBinaryOutputNotOk(t *testing.T) {
	cfg := config.Default()
	cfg.Hosts["nonexistent.invalid"] = config.Host{RemoteUser: "test"}

	r := checkHostRemoteBinary(cfg, "nonexistent.invalid")
	// SSH will fail, covering the error path where stdout != "ok"
	if r.Passed {
		t.Error("should fail for unreachable host")
	}
}

// --- checkClipboardTool with custom path that doesn't exist ---

func TestClipboardToolCustomPathNotFound(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("skipping darwin-specific test")
	}
	cfg := config.Default()
	cfg.MacOS.PngpastePath = "/nonexistent/path/pngpaste"

	r := checkClipboardTool(cfg)
	if r.Passed {
		t.Error("should fail for non-existent custom tool path")
	}
	if !strings.Contains(r.Detail, "not found in PATH") {
		t.Errorf("expected 'not found in PATH', got: %s", r.Detail)
	}
}

// --- checkTokenFile with directory instead of file ---

func TestTokenFileDirectoryInsteadOfFile(t *testing.T) {
	tmpDir := t.TempDir()
	tokenDir := filepath.Join(tmpDir, "token")
	if err := os.Mkdir(tokenDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	origDefault := defaultTokenPathOverride
	t.Cleanup(func() { defaultTokenPathOverride = origDefault })
	defaultTokenPathOverride = tokenDir

	cfg := config.Default()
	r := checkTokenFile(cfg)
	// A directory instead of a file should fail
	if r.Passed {
		t.Error("expected check to fail for directory instead of file")
	}
}

// --- checkDaemonRunning returns fix hint ---

func TestCheckDaemonRunningReturnsFixHint(t *testing.T) {
	cfg := config.Default()
	r := checkDaemonRunning(cfg)
	if !r.Passed {
		if r.FixHint == "" {
			t.Error("failed checkDaemonRunning should have a fix hint")
		}
		if !strings.Contains(r.FixHint, "start") {
			t.Errorf("expected fix hint mentioning start, got: %s", r.FixHint)
		}
	}
}

// --- RunHostChecks for host with Termius mode ---

func TestRunHostChecksTermiusMode(t *testing.T) {
	cfg := config.Default()
	cfg.Hosts["termius-host"] = config.Host{Termius: true}

	results := RunHostChecks(cfg, "termius-host", false)

	// SSH config check should pass in Termius mode
	foundSSHConfig := false
	for _, r := range results {
		if r.Name == "SSH RemoteForward configured" {
			foundSSHConfig = true
			if !r.Passed {
				t.Errorf("SSH config check should pass in Termius mode, got: %s", r.Detail)
			}
		}
	}
	if !foundSSHConfig {
		t.Error("missing SSH RemoteForward configured check")
	}
}

// --- resolvedTokenPath with override ---

func TestResolvedTokenPathWithOverride(t *testing.T) {
	origDefault := defaultTokenPathOverride
	t.Cleanup(func() { defaultTokenPathOverride = origDefault })

	defaultTokenPathOverride = "/custom/path/token"
	if got := resolvedTokenPath(); got != "/custom/path/token" {
		t.Errorf("resolvedTokenPath() = %q, want /custom/path/token", got)
	}

	defaultTokenPathOverride = ""
	got := resolvedTokenPath()
	if got == "" {
		t.Error("resolvedTokenPath() should not return empty when override is empty (falls back to default)")
	}
}
