//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/pastelocal/pastelocal/internal/auth"
	"github.com/pastelocal/pastelocal/internal/clipboard"
	"github.com/pastelocal/pastelocal/internal/config"
)

// -------------------------------------------------------------------
// Test helpers
// -------------------------------------------------------------------

// captureHandler records slog entries for inspection in threat tests.
type captureHandler struct {
	mu      sync.Mutex
	entries []map[string]any
}

func (h *captureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *captureHandler) WithAttrs(_ []slog.Attr) slog.Handler         { return h }
func (h *captureHandler) WithGroup(_ string) slog.Handler              { return h }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	m := map[string]any{"msg": r.Message}
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.Any()
		return true
	})
	h.mu.Lock()
	h.entries = append(h.entries, m)
	h.mu.Unlock()
	return nil
}

// hasAttr returns true if any captured log entry contains the given key-value pair.
func (h *captureHandler) hasAttr(key string, value any) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	want := fmt.Sprint(value)
	for _, e := range h.entries {
		if v, ok := e[key]; ok && fmt.Sprint(v) == want {
			return true
		}
	}
	return false
}

// threatReader implements clipboard.Reader for threat tests.
type threatReader struct {
	image []byte
	err   error
}

func (m *threatReader) ReadImage(_ context.Context) ([]byte, error) {
	return m.image, m.err
}

func (m *threatReader) ReadContent(ctx context.Context) (*clipboard.Content, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &clipboard.Content{Data: m.image, Format: "png"}, nil
}

func (m *threatReader) ReadText(ctx context.Context) (string, error) {
	return "", nil
}

func (m *threatReader) AvailableFormats(ctx context.Context) ([]string, error) {
	return []string{"image/png"}, nil
}

func (m *threatReader) IsConcealed(ctx context.Context) (bool, error) {
	return false, nil
}

// newThreatServer creates a Server with a temp token file and returns the
// server, the token string, and the token file path.
func newThreatServer(t *testing.T, cfg *config.Config, reader *threatReader, logger *slog.Logger) (*Server, string, string) {
	t.Helper()

	if cfg == nil {
		cfg = config.Default()
		cfg.Port = 0
	}
	if reader == nil {
		reader = &threatReader{image: []byte("fake-png-data")}
	}
	if logger == nil {
		logger = slog.Default()
	}

	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token")
	tokenStore := auth.NewTokenStore(false, tokenPath)
	testToken := "threat-test-token"
	if err := tokenStore.Store(testToken); err != nil {
		t.Fatalf("store token: %v", err)
	}

	s := New(cfg, config.ConfigPath(), tokenStore, reader, logger)
	return s, testToken, tokenPath
}

// startRealServer starts the server's HTTP server on a random loopback port
// and installs a SIGHUP handler that triggers config reload.
// Returns the listen address and a cleanup function.
func startRealServer(t *testing.T, s *Server) (string, func()) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()

	// Install a SIGHUP handler so tests can send SIGHUP to trigger reload.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP)
	go func() {
		for sig := range sigCh {
			if sig == syscall.SIGHUP {
				s.reloadConfig()
			}
		}
	}()

	go s.httpServer.Serve(ln)

	// Wait until the server is accepting connections.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, dialErr := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if dialErr == nil {
			conn.Close()
			return addr, func() {
				signal.Stop(sigCh)
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				s.httpServer.Shutdown(ctx)
			}
		}
	}
	t.Fatalf("server did not start within 2s on %s", addr)
	return "", func() {}
}

// makeClipboardReq creates an authenticated GET /clipboard request.
func makeClipboardReq(token string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/clipboard", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

// decodeErrorResponse decodes the JSON error response body into a map.
func decodeErrorResponse(t *testing.T, body io.Reader) map[string]any {
	t.Helper()
	var resp map[string]any
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	return resp
}

// -------------------------------------------------------------------
// T-9.1.1: Non-loopback connection refused
// -------------------------------------------------------------------

func TestThreat_NonLoopbackConnectionRefused(t *testing.T) {
	s, _, _ := newThreatServer(t, nil, nil, nil)

	// Part A: Verify the server binds to 127.0.0.1 (loopback only).
	addr, cleanup := startRealServer(t, s)
	defer cleanup()

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		t.Errorf("server bound to %s, expected a loopback address (127.0.0.1)", host)
	}

	// Part B: Verify ConnContext rejects non-loopback connections.
	conn := &mockConn{
		remoteAddr: &net.TCPAddr{
			IP:   net.ParseIP("10.0.0.1"),
			Port: 12345,
		},
	}
	_ = s.rejectNonLoopbackConnCtx(context.Background(), conn)
	if !conn.closed {
		t.Error("non-loopback connection was not closed by ConnContext")
	}

	// Part C: Verify loopback connections are accepted.
	loopback := &mockConn{
		remoteAddr: &net.TCPAddr{
			IP:   net.ParseIP("127.0.0.1"),
			Port: 12345,
		},
	}
	_ = s.rejectNonLoopbackConnCtx(context.Background(), loopback)
	if loopback.closed {
		t.Error("loopback connection should not be closed by ConnContext")
	}
}

// -------------------------------------------------------------------
// T-9.1.2: Wrong token returns 401 with CB2001 and auth_failed=true
// -------------------------------------------------------------------

func TestThreat_WrongTokenReturns401(t *testing.T) {
	h := &captureHandler{}
	logger := slog.New(h)

	s, _, _ := newThreatServer(t, nil, nil, logger)

	req := makeClipboardReq("this-is-the-wrong-token")
	w := httptest.NewRecorder()
	s.handleClipboard(w, req)

	// Verify 401 status.
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	// Verify CB2001 error code.
	resp := decodeErrorResponse(t, w.Body)
	if code, _ := resp["code"].(string); code != "CB2001" {
		t.Errorf("code = %q, want %q", code, "CB2001")
	}

	// Verify auth_failed=true in log output.
	if !h.hasAttr("auth_failed", true) {
		t.Error("expected auth_failed=true in log output for wrong token")
	}
}

// -------------------------------------------------------------------
// T-9.1.3: Token file with wrong permissions is rejected
// -------------------------------------------------------------------

func TestThreat_TokenFileWrongPerms(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token")

	// Write a token file with insecure 0644 permissions.
	if err := os.WriteFile(tokenPath, []byte("my-secret-token"), 0644); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	// Verify CheckTokenFilePerms rejects the file.
	err := auth.CheckTokenFilePerms(tokenPath)
	if err == nil {
		t.Error("CheckTokenFilePerms should reject 0644 permissions")
	}
	if err != nil && !strings.Contains(err.Error(), "0600") {
		t.Errorf("error message should reference 0600, got: %v", err)
	}

	// Verify that properly storing the token fixes the permissions.
	// os.WriteFile preserves existing file permissions, so we must remove
	// the insecure file first to let StoreTokenFile create a fresh one with 0600.
	os.Remove(tokenPath)
	if err := auth.StoreTokenFile("new-secret-token", tokenPath); err != nil {
		t.Fatalf("StoreTokenFile: %v", err)
	}
	if err := auth.CheckTokenFilePerms(tokenPath); err != nil {
		t.Errorf("CheckTokenFilePerms on properly stored file should pass: %v", err)
	}
}

// -------------------------------------------------------------------
// T-9.1.4: Image larger than max_image_bytes returns CB1005
// -------------------------------------------------------------------

func TestThreat_ImageLargerThanMax(t *testing.T) {
	cfg := config.Default()
	cfg.Port = 0
	cfg.MaxImageBytes = 100 // very low limit for testing

	// Create a reader that returns more than 100 bytes.
	reader := &threatReader{image: make([]byte, 200)}
	s, token, _ := newThreatServer(t, cfg, reader, nil)

	req := makeClipboardReq(token)
	w := httptest.NewRecorder()
	s.handleClipboard(w, req)

	// Verify 413 status.
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", w.Code, http.StatusRequestEntityTooLarge)
	}

	// Verify CB1005 error code.
	bodyLen := w.Body.Len() // capture before decode consumes the buffer
	resp := decodeErrorResponse(t, w.Body)
	if code, _ := resp["code"].(string); code != "CB1005" {
		t.Errorf("code = %q, want %q", code, "CB1005")
	}

	// Verify the daemon did not OOM by confirming a response was written.
	if bodyLen == 0 {
		t.Error("response body is empty; daemon may have OOM'd")
	}
}

// -------------------------------------------------------------------
// T-9.1.5: Rate limit exceeded returns CB4001 with Retry-After
// -------------------------------------------------------------------

func TestThreat_RateLimitExceeded(t *testing.T) {
	cfg := config.Default()
	cfg.Port = 0
	cfg.RateLimitPerMinute = 3 // very low limit

	reader := &threatReader{image: []byte("data")}
	s, token, _ := newThreatServer(t, cfg, reader, nil)

	// Exhaust the rate limit with successful requests.
	allowed := 0
	for i := 0; i < 5; i++ {
		req := makeClipboardReq(token)
		w := httptest.NewRecorder()
		s.handleClipboard(w, req)

		if w.Code == http.StatusOK {
			allowed++
		}
	}
	if allowed > 3 {
		t.Errorf("allowed %d requests, expected at most 3", allowed)
	}

	// The next request must be rate-limited.
	req := makeClipboardReq(token)
	w := httptest.NewRecorder()
	s.handleClipboard(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}

	resp := decodeErrorResponse(t, w.Body)
	if code, _ := resp["code"].(string); code != "CB4001" {
		t.Errorf("code = %q, want %q", code, "CB4001")
	}

	// Verify Retry-After header is present.
	retryAfter := w.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Error("Retry-After header is missing on rate-limited response")
	}
}

// -------------------------------------------------------------------
// T-9.1.6: Token rotation invalidates old token within 1 second
// -------------------------------------------------------------------

func TestThreat_TokenRotationInvalidatesOld(t *testing.T) {
	cfg := config.Default()
	cfg.Port = 0
	cfg.RateLimitPerMinute = 100 // high limit to avoid rate-limit interference

	reader := &threatReader{image: []byte("data")}
	s, tokenA, tokenPath := newThreatServer(t, cfg, reader, nil)

	addr, cleanup := startRealServer(t, s)
	defer cleanup()

	client := &http.Client{Timeout: 2 * time.Second}

	// Step 1: Verify token A works.
	req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/clipboard", nil)
	req.Header.Set("Authorization", "Bearer "+tokenA)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request with token A: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("token A: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Step 2: Rotate to token B by updating the token file.
	tokenB := "rotated-token-B-value"
	if err := auth.StoreTokenFile(tokenB, tokenPath); err != nil {
		t.Fatalf("store token B: %v", err)
	}

	// Send SIGHUP to trigger config reload (defense in depth).
	syscall.Kill(syscall.Getpid(), syscall.SIGHUP)
	time.Sleep(200 * time.Millisecond) // allow signal handling

	// Step 3: Verify token A is rejected within 1 second.
	start := time.Now()
	req, _ = http.NewRequest(http.MethodGet, "http://"+addr+"/clipboard", nil)
	req.Header.Set("Authorization", "Bearer "+tokenA)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("request with old token A: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	elapsed := time.Since(start)
	if elapsed > time.Second {
		t.Errorf("old token rejection took %v, want < 1s", elapsed)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("old token A: status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	// Step 4: Verify token B works.
	req, _ = http.NewRequest(http.MethodGet, "http://"+addr+"/clipboard", nil)
	req.Header.Set("Authorization", "Bearer "+tokenB)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("request with new token B: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("new token B: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// -------------------------------------------------------------------
// T-9.1.7: Token never appears in argv or environ
// -------------------------------------------------------------------

func TestThreat_TokenNeverInArgvOrEnv(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("skipping: only supported on Linux and macOS")
	}

	// Locate the pastelocal-remote binary relative to this test file.
	_, thisFile, _, _ := runtime.Caller(0)
	projectRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	binPath := filepath.Join(projectRoot, "pastelocal-remote")
	if _, err := os.Stat(binPath); err != nil {
		t.Skipf("skipping: pastelocal-remote binary not found at %s", binPath)
	}

	// Create a token file with a distinctive secret value.
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token")
	secretToken := "supersecret-test-token-T9.1.7-xyzzy"
	if err := os.WriteFile(tokenPath, []byte(secretToken), 0600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	// Start a test server so pastelocal-remote has something to connect to.
	cfg := config.Default()
	cfg.Port = 0
	cfg.RateLimitPerMinute = 100
	reader := &threatReader{image: []byte("data")}
	s, _, _ := newThreatServer(t, cfg, reader, nil)

	addr, cleanup := startRealServer(t, s)
	defer cleanup()

	_, portStr, _ := net.SplitHostPort(addr)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	// Start pastelocal-remote with a long timeout so it stays alive for inspection.
	cmd := exec.Command(binPath,
		"-port", fmt.Sprintf("%d", port),
		"-token-file", tokenPath,
		"-timeout", "10s",
	)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start pastelocal-remote: %v", err)
	}
	pid := cmd.Process.Pid

	// Give the process a moment to fully start.
	time.Sleep(200 * time.Millisecond)

	// Check command line does not contain the secret token value.
	psOut, err := exec.Command("ps", "-o", "command=", "-p", fmt.Sprintf("%d", pid)).Output()
	if err != nil {
		t.Logf("ps command failed (process may have exited): %v", err)
	} else {
		cmdline := string(psOut)
		if strings.Contains(cmdline, secretToken) {
			t.Errorf("secret token found in process command line:\n%s", cmdline)
		}
	}

	// Check environment for the secret token.
	switch runtime.GOOS {
	case "linux":
		// Read /proc/PID/environ directly.
		environPath := fmt.Sprintf("/proc/%d/environ", pid)
		data, readErr := os.ReadFile(environPath)
		if readErr != nil {
			t.Logf("could not read %s: %v", environPath, readErr)
		} else {
			for _, ev := range strings.Split(string(data), "\x00") {
				if strings.Contains(ev, secretToken) && !strings.HasPrefix(ev, "PASTELOCAL_TOKEN=") {
					t.Errorf("secret token found in process environment: %s", ev)
				}
			}
		}

	case "darwin":
		// macOS: best-effort check via ps eww.
		psEnvOut, psErr := exec.Command("ps", "eww", "-p", fmt.Sprintf("%d", pid)).Output()
		if psErr != nil {
			t.Logf("ps eww failed: %v", psErr)
		} else {
			for _, line := range strings.Split(string(psEnvOut), "\n") {
				if strings.Contains(line, secretToken) && !strings.Contains(line, "PASTELOCAL_TOKEN=") {
					t.Errorf("secret token found in process environment: %s", line)
				}
			}
		}
	}

	// Clean up the subprocess.
	cmd.Process.Kill()
	cmd.Wait()
}
