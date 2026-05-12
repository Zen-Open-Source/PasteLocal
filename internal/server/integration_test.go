//go:build integration

package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/clipbridge/clipbridge/internal/auth"
	"github.com/clipbridge/clipbridge/internal/config"
	"github.com/clipbridge/clipbridge/internal/proto"
)

// testEnv manages the lifecycle of an integration test server.
type testEnv struct {
	dir        string
	cfg        *config.Config
	cfgPath    string
	tokenPath  string
	token      string
	tokenStore *auth.TokenStore
	server     *Server
	baseURL    string
	port       int
}

// newTestEnv creates a temp directory, writes config and token, and returns
// a testEnv ready to have startServer called on it. The caller must call
// env.cleanup() when done.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	dir, err := os.MkdirTemp("", "clipbridge-integration-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}

	port := findFreePort(t)

	cfg := config.Default()
	cfg.Port = port

	cfgPath := filepath.Join(dir, "config.toml")
	if err := config.Save(cfg, cfgPath); err != nil {
		os.RemoveAll(dir)
		t.Fatalf("save config: %v", err)
	}

	token, err := generateTestToken()
	if err != nil {
		os.RemoveAll(dir)
		t.Fatalf("generate token: %v", err)
	}

	tokenPath := filepath.Join(dir, "token")
	tokenStore := auth.NewTokenStore(false, tokenPath)
	if err := tokenStore.Store(token); err != nil {
		os.RemoveAll(dir)
		t.Fatalf("store token: %v", err)
	}

	return &testEnv{
		dir:        dir,
		cfg:        cfg,
		cfgPath:    cfgPath,
		tokenPath:  tokenPath,
		token:      token,
		tokenStore: tokenStore,
		port:       port,
	}
}

// startServer creates the Server and starts it in a goroutine. It waits for
// the /health endpoint to respond before returning.
func (e *testEnv) startServer(t *testing.T) {
	t.Helper()

	reader := &mockReader{image: []byte("fake-png-data-for-integration-test")}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	e.server = New(e.cfg, e.cfgPath, e.tokenStore, reader, logger)

	go func() {
		if err := e.server.Start(); err != nil && err != http.ErrServerClosed {
			t.Logf("server start: %v", err)
		}
	}()

	e.baseURL = fmt.Sprintf("http://127.0.0.1:%d", e.port)
	e.waitForHealth(t)
}

// waitForHealth polls /health until the server responds or the deadline passes.
func (e *testEnv) waitForHealth(t *testing.T) {
	t.Helper()

	client := &http.Client{Timeout: 1 * time.Second}
	deadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(deadline) {
		resp, err := client.Get(e.baseURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("server did not become healthy within 10s")
}

// cleanup shuts down the server and removes the temp directory.
func (e *testEnv) cleanup(t *testing.T) {
	t.Helper()

	if e.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := e.server.Shutdown(ctx); err != nil {
			t.Logf("server shutdown: %v", err)
		}
	}

	if e.dir != "" {
		os.RemoveAll(e.dir)
	}
}

// findFreePort returns an available TCP port on 127.0.0.1.
func findFreePort(t *testing.T) int {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// generateTestToken produces a random URL-safe base64 token for tests.
func generateTestToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// -------------------------------------------------------------------
// Integration tests
// -------------------------------------------------------------------

// TestIntegration_FullClipboardRequest spins up a real server with a temp
// config and token, then makes a GET /clipboard with correct auth and
// verifies the response shape.
func TestIntegration_FullClipboardRequest(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup(t)
	env.startServer(t)

	req, err := http.NewRequest(http.MethodGet, env.baseURL+"/clipboard", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+env.token)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result proto.ClipboardResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if !result.OK {
		t.Error("ok = false, want true")
	}
	if result.Image == "" {
		t.Error("image is empty, want base64-encoded data")
	}
	if result.Format != "png" {
		t.Errorf("format = %q, want %q", result.Format, "png")
	}
	if result.ByteCount == 0 {
		t.Error("byte_count = 0, want > 0")
	}
	if result.CapturedAt == "" {
		t.Error("captured_at is empty, want RFC3339 timestamp")
	}
}

// TestIntegration_HealthEndpoint verifies GET /health returns 200 and
// {"ok": true}.
func TestIntegration_HealthEndpoint(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup(t)
	env.startServer(t)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(env.baseURL + "/health")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result proto.HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if !result.OK {
		t.Error("ok = false, want true")
	}
}

// TestIntegration_VersionEndpoint verifies GET /version returns
// protocol_version and binary_version.
func TestIntegration_VersionEndpoint(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup(t)
	env.startServer(t)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(env.baseURL + "/version")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result proto.VersionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if result.ProtocolVersion != proto.ProtocolVersion {
		t.Errorf("protocol_version = %d, want %d", result.ProtocolVersion, proto.ProtocolVersion)
	}
	if result.BinaryVersion == "" {
		t.Error("binary_version is empty, want a non-empty version string")
	}
}

// TestIntegration_AuthTokenMismatch verifies that a wrong token returns 401
// with error code CB2001.
func TestIntegration_AuthTokenMismatch(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup(t)
	env.startServer(t)

	req, err := http.NewRequest(http.MethodGet, env.baseURL+"/clipboard", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer wrong-token-value")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if code, _ := result["code"].(string); code != "CB2001" {
		t.Errorf("code = %q, want %q", code, "CB2001")
	}
}

// TestIntegration_LoopbackBinding verifies the server binds to 127.0.0.1
// only, not 0.0.0.0.
func TestIntegration_LoopbackBinding(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup(t)
	env.startServer(t)

	// Verify we can connect via 127.0.0.1.
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", env.port), 5*time.Second)
	if err != nil {
		t.Fatalf("connect to 127.0.0.1:%d: %v", env.port, err)
	}
	conn.Close()

	// Verify the server is NOT listening on 0.0.0.0 by trying the
	// machine's non-loopback IP addresses. If the server were bound to
	// 0.0.0.0, a connection to a LAN IP would succeed (and then be
	// rejected by ConnContext). If it is bound to 127.0.0.1 only, the
	// TCP connection itself will be refused.
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatalf("get interface addrs: %v", err)
	}

	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() || ipNet.IP.IsUnspecified() {
			continue
		}
		// The server binds to IPv4 127.0.0.1 only; skip IPv6 addresses.
		if ipNet.IP.To4() == nil {
			continue
		}
		target := net.JoinHostPort(ipNet.IP.String(), fmt.Sprintf("%d", env.port))
		c, err := net.DialTimeout("tcp", target, 2*time.Second)
		if err == nil {
			c.Close()
			// Connection succeeded via non-loopback IP, meaning the
			// server is listening on 0.0.0.0. This is a failure.
			t.Errorf("server accepted connection via non-loopback IP %s; should bind to 127.0.0.1 only", ipNet.IP)
		}
		// Connection refused is the expected outcome — the server is
		// not listening on this interface.
	}
}

// TestIntegration_RateLimiter hits /clipboard rapidly and verifies CB4001
// is returned after the configured threshold is exceeded.
func TestIntegration_RateLimiter(t *testing.T) {
	dir, err := os.MkdirTemp("", "clipbridge-integration-ratelimit-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	port := findFreePort(t)

	cfg := config.Default()
	cfg.Port = port
	cfg.RateLimitPerMinute = 3 // very low limit for testing

	cfgPath := filepath.Join(dir, "config.toml")
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}

	token, err := generateTestToken()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	tokenPath := filepath.Join(dir, "token")
	tokenStore := auth.NewTokenStore(false, tokenPath)
	if err := tokenStore.Store(token); err != nil {
		t.Fatalf("store token: %v", err)
	}

	reader := &mockReader{image: []byte("data")}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := New(cfg, cfgPath, tokenStore, reader, logger)

	go func() {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			t.Logf("server start: %v", err)
		}
	}()

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 5 * time.Second}

	// Wait for server to become healthy.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Hit /clipboard rapidly. The bucket starts full (3 tokens) so the
	// first 3 requests should succeed and subsequent ones should be
	// rate-limited with CB4001.
	rateLimited := false
	for i := 0; i < 10; i++ {
		req, _ := http.NewRequest(http.MethodGet, baseURL+"/clipboard", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := client.Do(req)
		if err != nil {
			t.Logf("request %d error: %v", i, err)
			continue
		}

		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		if code, _ := result["code"].(string); code == "CB4001" {
			rateLimited = true
			break
		}
	}

	if !rateLimited {
		t.Error("never received CB4001 rate limit response after rapid requests")
	}

	// Shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

// TestIntegration_SIGHUPReload sends SIGHUP, changes the token file, and
// verifies the old token is rejected while the new token works.
func TestIntegration_SIGHUPReload(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup(t)
	env.startServer(t)

	client := &http.Client{Timeout: 5 * time.Second}

	// Verify the current token works.
	req, _ := http.NewRequest(http.MethodGet, env.baseURL+"/clipboard", nil)
	req.Header.Set("Authorization", "Bearer "+env.token)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request with correct token failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status with correct token = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Change the token in the file.
	newToken, err := generateTestToken()
	if err != nil {
		t.Fatalf("generate new token: %v", err)
	}
	if err := auth.StoreTokenFile(newToken, env.tokenPath); err != nil {
		t.Fatalf("store new token: %v", err)
	}

	// Send SIGHUP to trigger config/token reload.
	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("find process: %v", err)
	}
	p.Signal(syscall.SIGHUP)

	// Give the signal handler time to process.
	time.Sleep(500 * time.Millisecond)

	// Verify the old token is now rejected.
	req, _ = http.NewRequest(http.MethodGet, env.baseURL+"/clipboard", nil)
	req.Header.Set("Authorization", "Bearer "+env.token)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("request with old token failed: %v", err)
	}

	var errResp map[string]any
	json.NewDecoder(resp.Body).Decode(&errResp)
	resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status with old token = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
	if code, _ := errResp["code"].(string); code != "CB2001" {
		t.Errorf("code with old token = %q, want %q", code, "CB2001")
	}

	// Verify the new token works.
	req, _ = http.NewRequest(http.MethodGet, env.baseURL+"/clipboard", nil)
	req.Header.Set("Authorization", "Bearer "+newToken)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("request with new token failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status with new token = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// TestIntegration_SIGTERMGraceful sends SIGTERM and verifies the server
// shuts down within 5 seconds.
func TestIntegration_SIGTERMGraceful(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup(t)
	env.startServer(t)

	client := &http.Client{Timeout: 5 * time.Second}

	// Verify the server is up before sending SIGTERM.
	resp, err := client.Get(env.baseURL + "/health")
	if err != nil {
		t.Fatalf("health check before SIGTERM: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health check status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Send SIGTERM to the process. The signal handler installed by
	// handleSignals will catch it and gracefully shut down the server.
	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("find process: %v", err)
	}
	p.Signal(syscall.SIGTERM)

	// Wait for the server to stop accepting connections (within 5s).
	shutdownDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(shutdownDeadline) {
		resp, err := client.Get(env.baseURL + "/health")
		if err != nil {
			// Server has stopped accepting connections.
			break
		}
		resp.Body.Close()
		time.Sleep(100 * time.Millisecond)
	}

	// Final check: server should be down.
	_, err = net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", env.port), 1*time.Second)
	if err == nil {
		t.Error("server is still accepting connections 5s after SIGTERM")
	}
}
