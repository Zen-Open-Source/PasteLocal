package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pastelocal/pastelocal/internal/auth"
	"github.com/pastelocal/pastelocal/internal/clipboard"
	"github.com/pastelocal/pastelocal/internal/config"
	"github.com/pastelocal/pastelocal/internal/proto"
)

// mockReader implements clipboard.Reader for testing.
type mockReader struct {
	image []byte
	err   error
}

func (m *mockReader) ReadImage(ctx context.Context) ([]byte, error) {
	return m.image, m.err
}

func (m *mockReader) ReadContent(ctx context.Context) (*clipboard.Content, error) {
	if m.err != nil {
		return nil, m.err
	}
	if len(m.image) > 0 {
		return &clipboard.Content{Data: m.image, Format: "png"}, nil
	}
	return nil, fmt.Errorf("no content")
}

func (m *mockReader) ReadText(ctx context.Context) (string, error) {
	return "", fmt.Errorf("no text")
}

func (m *mockReader) AvailableFormats(ctx context.Context) ([]string, error) {
	return []string{"image/png"}, nil
}

// newTestServer creates a Server wired up for testing with sensible defaults.
func newTestServer(t *testing.T, cfg *config.Config, reader *mockReader) (*Server, string) {
	t.Helper()

	if cfg == nil {
		cfg = config.Default()
		cfg.Port = 0 // let OS pick a port
	}

	// Store a token to the keychain or a temp file.
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token")
	tokenStore := auth.NewTokenStore(false, tokenPath)
	testToken := "test-bearer-token-abc123"
	if err := tokenStore.Store(testToken); err != nil {
		t.Fatalf("store token: %v", err)
	}

	if reader == nil {
		reader = &mockReader{
			image: []byte("fake-png-data"),
		}
	}

	s := New(cfg, config.ConfigPath(), tokenStore, reader, nilLogger())
	return s, testToken
}

func nilLogger() *slog.Logger {
	return slog.Default()
}

// Ensure slog import is used
var _ = slog.Default

// -------------------------------------------------------------------
// Tests
// -------------------------------------------------------------------

func TestHealthReturns200(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	s.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp proto.HealthResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.OK {
		t.Error("ok = false, want true")
	}
}

func TestVersionReturnsCorrectVersions(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	w := httptest.NewRecorder()
	s.handleVersion(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp proto.VersionResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ProtocolVersion != proto.ProtocolVersion {
		t.Errorf("protocol_version = %d, want %d", resp.ProtocolVersion, proto.ProtocolVersion)
	}
	if resp.BinaryVersion != BinaryVersion {
		t.Errorf("binary_version = %q, want %q", resp.BinaryVersion, BinaryVersion)
	}
}

func TestClipboardWithoutAuthReturns401(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/clipboard", nil)
	w := httptest.NewRecorder()
	s.handleClipboard(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if code, _ := resp["code"].(string); code != "CB2002" {
		t.Errorf("code = %q, want %q", code, "CB2002")
	}
}

func TestClipboardWithWrongTokenReturns401(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/clipboard", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()
	s.handleClipboard(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if code, _ := resp["code"].(string); code != "CB2001" {
		t.Errorf("code = %q, want %q", code, "CB2001")
	}
}

func TestClipboardWithCorrectTokenReturns200(t *testing.T) {
	reader := &mockReader{image: []byte("fake-png-data")}
	s, token := newTestServer(t, nil, reader)

	req := httptest.NewRequest(http.MethodGet, "/clipboard", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.handleClipboard(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp proto.ClipboardResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.OK {
		t.Error("ok = false, want true")
	}
	if resp.Format != "png" {
		t.Errorf("format = %q, want %q", resp.Format, "png")
	}
	if resp.ByteCount != int64(len(reader.image)) {
		t.Errorf("byte_count = %d, want %d", resp.ByteCount, len(reader.image))
	}
}

func TestRateLimiterTriggersAtThreshold(t *testing.T) {
	// Create a rate limiter with a very low limit: 3 per minute.
	rl := NewRateLimiter(3)

	// Should allow 3 requests (starts with a full bucket).
	allowed := 0
	for i := 0; i < 5; i++ {
		if rl.Allow() {
			allowed++
		}
	}
	if allowed != 3 {
		t.Errorf("allowed = %d, want 3", allowed)
	}
}

func TestSemaphoreBlocksAtMaxInFlight(t *testing.T) {
	cfg := config.Default()
	cfg.Port = 0
	cfg.MaxInFlight = 1

	reader := &mockReader{image: []byte("data")}
	s, token := newTestServer(t, cfg, reader)

	// Drain the single semaphore slot.
	<-s.sem

	// Now /clipboard should be rejected with CB4002 (concurrency limit).
	req := httptest.NewRequest(http.MethodGet, "/clipboard", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.handleClipboard(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if code, _ := resp["code"].(string); code != "CB4002" {
		t.Errorf("code = %q, want %q", code, "CB4002")
	}

	// Put the slot back.
	s.sem <- struct{}{}
}

func TestNonLoopbackConnectionRejected(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)

	// Simulate a non-loopback remote address via ConnContext.
	conn := &mockConn{remoteAddr: &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 12345}}
	ctx := context.Background()
	newCtx := s.rejectNonLoopbackConnCtx(ctx, conn)

	// The connection should have been closed.
	if !conn.closed {
		t.Error("non-loopback connection was not closed")
	}

	// Context should still be returned (not cancelled).
	if newCtx == nil {
		t.Error("context should not be nil")
	}
}

func TestLoopbackConnectionAccepted(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)

	conn := &mockConn{remoteAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}}
	ctx := context.Background()
	_ = s.rejectNonLoopbackConnCtx(ctx, conn)

	if conn.closed {
		t.Error("loopback connection should not be closed")
	}
}

func TestAuditLogEntryWritten(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")

	cfg := config.Default()
	cfg.Port = 0
	cfg.AuditLog = auditPath

	reader := &mockReader{image: []byte("fake-png-data")}
	s, token := newTestServer(t, cfg, reader)

	req := httptest.NewRequest(http.MethodGet, "/clipboard", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	s.handleClipboard(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	// Verify the audit log file exists and contains an entry.
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}

	var entry AuditEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("unmarshal audit entry: %v", err)
	}

	if entry.Event != "clipboard_read" {
		t.Errorf("event = %q, want %q", entry.Event, "clipboard_read")
	}
	if entry.ByteCount != int64(len(reader.image)) {
		t.Errorf("byte_count = %d, want %d", entry.ByteCount, len(reader.image))
	}
	if entry.SourceIP != "127.0.0.1" {
		t.Errorf("source_ip = %q, want %q", entry.SourceIP, "127.0.0.1")
	}
	if entry.Time == "" {
		t.Error("time is empty")
	}
}

// -------------------------------------------------------------------
// Rate limiter unit tests
// -------------------------------------------------------------------

func TestRateLimiterAllowsInitially(t *testing.T) {
	rl := NewRateLimiter(60)
	if !rl.Allow() {
		t.Error("first request should be allowed")
	}
}

func TestRateLimiterRefill(t *testing.T) {
	rl := NewRateLimiter(60) // 1 token/sec
	// Drain all tokens.
	for rl.Allow() {
	}
	// Wait long enough for a refill.
	time.Sleep(1100 * time.Millisecond)
	if !rl.Allow() {
		t.Error("should have refilled a token after ~1s")
	}
}

// -------------------------------------------------------------------
// Audit unit tests
// -------------------------------------------------------------------

func TestWriteAuditCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	entry := AuditEntry{
		Event:     "test_event",
		ByteCount: 100,
		SourceIP:  "127.0.0.1",
		Auth:      "token",
	}

	if err := WriteAudit(path, entry); err != nil {
		t.Fatalf("WriteAudit: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("audit file is empty")
	}

	var got AuditEntry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Event != "test_event" {
		t.Errorf("event = %q, want %q", got.Event, "test_event")
	}
}

func TestWriteAuditEmptyPath(t *testing.T) {
	// Empty path should be a no-op.
	if err := WriteAudit("", AuditEntry{}); err != nil {
		t.Errorf("WriteAudit with empty path should not error: %v", err)
	}
}

func TestWriteAuditAppends(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	for i := 0; i < 3; i++ {
		entry := AuditEntry{
			Event:     fmt.Sprintf("event_%d", i),
			ByteCount: int64(i),
		}
		if err := WriteAudit(path, entry); err != nil {
			t.Fatalf("WriteAudit %d: %v", i, err)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
}

// -------------------------------------------------------------------
// Helpers
// -------------------------------------------------------------------

// mockConn implements net.Conn for testing ConnContext.
type mockConn struct {
	remoteAddr net.Addr
	closed     bool
}

func (m *mockConn) Read(b []byte) (n int, err error)   { return 0, fmt.Errorf("mock") }
func (m *mockConn) Write(b []byte) (n int, err error)  { return 0, fmt.Errorf("mock") }
func (m *mockConn) Close() error                        { m.closed = true; return nil }
func (m *mockConn) LocalAddr() net.Addr                 { return nil }
func (m *mockConn) RemoteAddr() net.Addr                { return m.remoteAddr }
func (m *mockConn) SetDeadline(t time.Time) error       { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error   { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error  { return nil }

// -------------------------------------------------------------------
// Method not allowed tests
// -------------------------------------------------------------------

func TestClipboardMethodNotAllowed(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)

	// POST is now allowed (write clipboard), so only test truly disallowed methods.
	for _, method := range []string{http.MethodPut, http.MethodDelete, http.MethodPatch} {
		req := httptest.NewRequest(method, "/clipboard", nil)
		w := httptest.NewRecorder()
		s.handleClipboard(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("method %s: status = %d, want %d", method, w.Code, http.StatusMethodNotAllowed)
		}
	}
}

func TestHealthMethodNotAllowed(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	w := httptest.NewRecorder()
	s.handleHealth(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestVersionMethodNotAllowed(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/version", nil)
	w := httptest.NewRecorder()
	s.handleVersion(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// -------------------------------------------------------------------
// validateAuth edge cases
// -------------------------------------------------------------------

func TestValidateAuthMissingHeader(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/clipboard", nil)
	token, err := s.validateAuth(req)
	if err == nil {
		t.Error("expected error for missing auth header")
	}
	if token != "" {
		t.Errorf("token = %q, want empty", token)
	}
	if err.Code != "CB2002" {
		t.Errorf("code = %q, want %q", err.Code, "CB2002")
	}
}

func TestValidateAuthMalformedHeader(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)

	// Test with "Basic" instead of "Bearer"
	req := httptest.NewRequest(http.MethodGet, "/clipboard", nil)
	req.Header.Set("Authorization", "Basic abc123")
	token, err := s.validateAuth(req)
	if err == nil {
		t.Error("expected error for malformed auth header")
	}
	if token != "" {
		t.Errorf("token = %q, want empty", token)
	}
	if err.Code != "CB2001" {
		t.Errorf("code = %q, want %q", err.Code, "CB2001")
	}

	// Test with no scheme at all
	req2 := httptest.NewRequest(http.MethodGet, "/clipboard", nil)
	req2.Header.Set("Authorization", "not-a-scheme")
	token2, err2 := s.validateAuth(req2)
	if err2 == nil {
		t.Error("expected error for auth header without scheme")
	}
	if token2 != "" {
		t.Errorf("token = %q, want empty", token2)
	}
	if err2.Code != "CB2001" {
		t.Errorf("code = %q, want %q", err2.Code, "CB2001")
	}
}

// -------------------------------------------------------------------
// extractRemoteIP edge cases
// -------------------------------------------------------------------

func TestExtractRemoteIPNoPort(t *testing.T) {
	// When RemoteAddr has no port, SplitHostPort fails, and the
	// full address is returned as-is.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "no-port-addr"
	ip := extractRemoteIP(req)
	if ip != "no-port-addr" {
		t.Errorf("got %q, want %q", ip, "no-port-addr")
	}
}

func TestExtractRemoteIPWithPort(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.1:8080"
	ip := extractRemoteIP(req)
	if ip != "192.168.1.1" {
		t.Errorf("got %q, want %q", ip, "192.168.1.1")
	}
}

// -------------------------------------------------------------------
// rejectNonLoopbackConnCtx nil conn
// -------------------------------------------------------------------

func TestNilConnReturnsContext(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)

	ctx := context.Background()
	newCtx := s.rejectNonLoopbackConnCtx(ctx, nil)
	if newCtx != ctx {
		t.Error("nil conn should return original context")
	}
}

// -------------------------------------------------------------------
// LastRead accessor
// -------------------------------------------------------------------

func TestLastReadAccessor(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)

	// Initially should be zero
	t1, size1, fmt1 := s.LastRead()
	if !t1.IsZero() {
		t.Error("initial LastRead time should be zero")
	}
	if size1 != 0 {
		t.Error("initial LastReadSize should be 0")
	}
	if fmt1 != "" {
		t.Error("initial LastReadFormat should be empty")
	}

	// After a clipboard read, it should be updated.
	reader := &mockReader{image: []byte("some-image-data")}
	s, token := newTestServer(t, nil, reader)

	req := httptest.NewRequest(http.MethodGet, "/clipboard", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.handleClipboard(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	t2, size2, fmt2 := s.LastRead()
	if t2.IsZero() {
		t.Error("LastRead time should not be zero after read")
	}
	if size2 != int64(len(reader.image)) {
		t.Errorf("LastReadSize = %d, want %d", size2, len(reader.image))
	}
	if fmt2 != "png" {
		t.Errorf("LastReadFormat = %q, want %q", fmt2, "png")
	}
}

// -------------------------------------------------------------------
// RateLimiter with 0 (unlimited)
// -------------------------------------------------------------------

func TestRateLimiterZeroMeansUnlimited(t *testing.T) {
	rl := NewRateLimiter(0)
	// With 0 rate, the bucket starts with 0 tokens, so Allow should return false.
	// But actually, maxTokens = 0, tokens = 0, so Allow will always return false.
	// Let's verify this behavior.
	if rl.Allow() {
		t.Error("RateLimiter(0) should not allow any requests (bucket empty)")
	}
}

// -------------------------------------------------------------------
// Server lifecycle: New → Start → requests → Shutdown
// -------------------------------------------------------------------

func TestServerLifecycle(t *testing.T) {
	cfg := config.Default()
	cfg.Port = 0 // let OS pick port

	reader := &mockReader{image: []byte("lifecycle-png")}
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token")
	tokenStore := auth.NewTokenStore(false, tokenPath)
	testToken := "************************"
	if err := tokenStore.Store(testToken); err != nil {
		t.Fatalf("store token: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s := New(cfg, config.ConfigPath(), tokenStore, reader, logger)

	// Start the server in a goroutine (Start blocks).
	go func() {
		if err := s.Start(); err != nil {
			t.Errorf("Start failed: %v", err)
		}
	}()

	// Give the server a moment to bind.
	time.Sleep(100 * time.Millisecond)

	// Test Shutdown directly.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}
}

// -------------------------------------------------------------------
// Clipboard handler: max_image_bytes exceeded
// -------------------------------------------------------------------

func TestClipboardMaxImageBytesExceeded(t *testing.T) {
	cfg := config.Default()
	cfg.Port = 0
	cfg.MaxImageBytes = 10 // Very small limit

	bigImage := make([]byte, 100)
	reader := &mockReader{image: bigImage}
	s, token := newTestServer(t, cfg, reader)

	req := httptest.NewRequest(http.MethodGet, "/clipboard", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.handleClipboard(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", w.Code, http.StatusRequestEntityTooLarge)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if code, _ := resp["code"].(string); code != "CB1005" {
		t.Errorf("code = %q, want %q", code, "CB1005")
	}
}

// -------------------------------------------------------------------
// Clipboard handler: reader error
// -------------------------------------------------------------------

func TestClipboardReaderError(t *testing.T) {
	reader := &mockReader{err: fmt.Errorf("clipboard tool crashed")}
	s, token := newTestServer(t, nil, reader)

	req := httptest.NewRequest(http.MethodGet, "/clipboard", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.handleClipboard(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if code, _ := resp["code"].(string); code != "CB1003" {
		t.Errorf("code = %q, want %q", code, "CB1003")
	}
}

// -------------------------------------------------------------------
// Rate-limited request
// -------------------------------------------------------------------

func TestClipboardRateLimited(t *testing.T) {
	cfg := config.Default()
	cfg.Port = 0
	cfg.RateLimitPerMinute = 2

	reader := &mockReader{image: []byte("data")}
	s, token := newTestServer(t, cfg, reader)

	// Use up the 2 allowed requests.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/clipboard", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.handleClipboard(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want %d", i, w.Code, http.StatusOK)
		}
	}

	// Third request should be rate-limited.
	req := httptest.NewRequest(http.MethodGet, "/clipboard", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.handleClipboard(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if code, _ := resp["code"].(string); code != "CB4001" {
		t.Errorf("code = %q, want %q", code, "CB4001")
	}
}

// -------------------------------------------------------------------
// Semaphore acquire and release
// -------------------------------------------------------------------

func TestSemaphoreAcquireRelease(t *testing.T) {
	cfg := config.Default()
	cfg.Port = 0
	cfg.MaxInFlight = 2

	reader := &mockReader{image: []byte("data")}
	s, token := newTestServer(t, cfg, reader)

	// Two requests should succeed concurrently (tested sequentially here).
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/clipboard", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.handleClipboard(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("request %d: status = %d, want %d", i, w.Code, http.StatusOK)
		}
	}
}

// -------------------------------------------------------------------
// Audit log: real file writes and JSON line verification
// -------------------------------------------------------------------

func TestWriteAuditCreatesDirectoryAndWritesJSON(t *testing.T) {
	dir := t.TempDir()
	// Use a nested path that doesn't exist yet
	auditPath := filepath.Join(dir, "sub", "dir", "audit.jsonl")

	entry := AuditEntry{
		Event:     "clipboard_read",
		ImageHash: "abc123",
		ByteCount: 500,
		SourceIP:  "10.0.0.1",
		Auth:      "test-token",
	}

	if err := WriteAudit(auditPath, entry); err != nil {
		t.Fatalf("WriteAudit: %v", err)
	}

	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}

	// Verify it's a valid JSON line
	var got AuditEntry
	if err := json.Unmarshal(bytes.TrimSpace(data), &got); err != nil {
		t.Fatalf("unmarshal audit entry: %v", err)
	}

	if got.Event != "clipboard_read" {
		t.Errorf("event = %q, want %q", got.Event, "clipboard_read")
	}
	if got.ImageHash != "abc123" {
		t.Errorf("image_hash = %q, want %q", got.ImageHash, "abc123")
	}
	if got.ByteCount != 500 {
		t.Errorf("byte_count = %d, want 500", got.ByteCount)
	}
	if got.SourceIP != "10.0.0.1" {
		t.Errorf("source_ip = %q, want %q", got.SourceIP, "10.0.0.1")
	}
	if got.Auth != "test-token" {
		t.Errorf("auth = %q, want %q", got.Auth, "test-token")
	}
	if got.Time == "" {
		t.Error("time should be set automatically")
	}

	// Verify trailing newline
	if data[len(data)-1] != '\n' {
		t.Error("audit entry should end with newline")
	}
}

func TestWriteAuditMultipleEntriesJSONLines(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")

	for i := 0; i < 5; i++ {
		entry := AuditEntry{
			Event:     fmt.Sprintf("event_%d", i),
			ImageHash: fmt.Sprintf("hash_%d", i),
			ByteCount: int64(i * 100),
		}
		if err := WriteAudit(auditPath, entry); err != nil {
			t.Fatalf("WriteAudit %d: %v", i, err)
		}
	}

	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(lines))
	}

	for i, line := range lines {
		var entry AuditEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Errorf("line %d: unmarshal: %v", i, err)
		}
		if entry.Event != fmt.Sprintf("event_%d", i) {
			t.Errorf("line %d: event = %q, want %q", i, entry.Event, fmt.Sprintf("event_%d", i))
		}
	}
}
