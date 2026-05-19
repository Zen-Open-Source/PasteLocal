package e2e

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"log/slog"

	"github.com/pastelocal/pastelocal/internal/auth"
	"github.com/pastelocal/pastelocal/internal/clipboard"
	"github.com/pastelocal/pastelocal/internal/config"
	"github.com/pastelocal/pastelocal/internal/proto"
	"github.com/pastelocal/pastelocal/internal/server"
)

// -------------------------------------------------------------------
// Test harness
// -------------------------------------------------------------------

type testHarness struct {
	server     *server.Server
	handler    http.Handler
	cfg        *config.Config
	token      string
	tokenStore *auth.TokenStore
	reader     *stubReader
	tmpDir     string
}

func newHarness(t *testing.T) *testHarness {
	t.Helper()

	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "token")
	tokenStore := auth.NewTokenStore(false, tokenPath)
	token, err := auth.GenerateToken()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if err := tokenStore.Store(token); err != nil {
		t.Fatalf("store token: %v", err)
	}

	cfg := config.Default()
	cfg.Port = 0
	cfg.AuditLog = filepath.Join(tmpDir, "audit.jsonl")
	cfg.History.Enabled = true
	cfg.History.Size = 5
	cfg.History.TTL = 3600

	reader := &stubReader{}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := server.New(cfg, filepath.Join(tmpDir, "config.toml"), tokenStore, reader, logger)

	// Create a ServeMux to route to the server's handlers.
	mux := http.NewServeMux()
	mux.HandleFunc("/clipboard", srv.HandleClipboard())
	mux.HandleFunc("/clipboard/history", srv.HandleClipboardHistory())
	mux.HandleFunc("/health", srv.HandleHealth())
	mux.HandleFunc("/version", srv.HandleVersion())

	return &testHarness{
		server:     srv,
		handler:    mux,
		cfg:        cfg,
		token:      token,
		tokenStore: tokenStore,
		reader:     reader,
		tmpDir:     tmpDir,
	}
}

// stubReader implements clipboard.Reader for E2E tests.
type stubReader struct {
	content *clipboard.Content
	err     error
}

func (r *stubReader) ReadImage(ctx context.Context) ([]byte, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.content != nil && r.content.Format == "png" {
		return r.content.Data, nil
	}
	return nil, fmt.Errorf("no image")
}

func (r *stubReader) ReadContent(ctx context.Context) (*clipboard.Content, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.content != nil {
		return r.content, nil
	}
	return nil, fmt.Errorf("no content")
}

func (r *stubReader) ReadText(ctx context.Context) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	if r.content != nil && r.content.Format == "text" {
		return string(r.content.Data), nil
	}
	return "", fmt.Errorf("no text")
}

func (r *stubReader) AvailableFormats(ctx context.Context) ([]string, error) {
	return []string{"image/png"}, nil
}

func (r *stubReader) IsConcealed(ctx context.Context) (bool, error) {
	return false, nil
}

// doRequest performs an authenticated HTTP request.
func (h *testHarness) doRequest(method, path string, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Authorization", "Bearer "+h.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	return w
}

// doRequestNoAuth performs an HTTP request without auth.
func (h *testHarness) doRequestNoAuth(method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	return w
}

// decodeResponse decodes a JSON response.
func decodeResponse(t *testing.T, w *httptest.ResponseRecorder, v interface{}) {
	t.Helper()
	if err := json.NewDecoder(w.Body).Decode(v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

// -------------------------------------------------------------------
// E2E Tests
// -------------------------------------------------------------------

func TestE2E_HealthCheck(t *testing.T) {
	h := newHarness(t)

	w := h.doRequestNoAuth(http.MethodGet, "/health")
	if w.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d", w.Code, http.StatusOK)
	}

	var health proto.HealthResponse
	decodeResponse(t, w, &health)
	if !health.OK {
		t.Error("health.OK = false, want true")
	}
}

func TestE2E_VersionCheck(t *testing.T) {
	h := newHarness(t)

	w := h.doRequestNoAuth(http.MethodGet, "/version")
	if w.Code != http.StatusOK {
		t.Fatalf("version status = %d, want %d", w.Code, http.StatusOK)
	}

	var ver proto.VersionResponse
	decodeResponse(t, w, &ver)
	if ver.ProtocolVersion != proto.ProtocolVersion {
		t.Errorf("protocol_version = %d, want %d", ver.ProtocolVersion, proto.ProtocolVersion)
	}
}

func TestE2E_ReadPNGClipboard(t *testing.T) {
	h := newHarness(t)
	h.reader.content = &clipboard.Content{
		Data:   createTestPNG(t),
		Format: "png",
	}

	w := h.doRequest(http.MethodGet, "/clipboard", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("clipboard status = %d, body: %s", w.Code, w.Body.String())
	}

	var clip proto.ClipboardResponse
	decodeResponse(t, w, &clip)
	if !clip.OK {
		t.Error("clip.OK = false")
	}
	if clip.Format != "png" {
		t.Errorf("format = %q, want %q", clip.Format, "png")
	}
	if clip.Image == "" {
		t.Error("image is empty")
	}
	if clip.ByteCount == 0 {
		t.Error("byte_count is 0")
	}
}

func TestE2E_ReadTextClipboard(t *testing.T) {
	h := newHarness(t)
	h.reader.content = &clipboard.Content{
		Data:   []byte("Hello from clipboard!"),
		Format: "text",
	}

	w := h.doRequest(http.MethodGet, "/clipboard", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("clipboard status = %d, body: %s", w.Code, w.Body.String())
	}

	var clip proto.ClipboardResponse
	decodeResponse(t, w, &clip)
	if clip.Format != "text" {
		t.Errorf("format = %q, want %q", clip.Format, "text")
	}
	if clip.Text != "Hello from clipboard!" {
		t.Errorf("text = %q, want %q", clip.Text, "Hello from clipboard!")
	}
	if clip.Image != "" {
		t.Error("image should be empty for text format")
	}
}

func TestE2E_WriteTextClipboard(t *testing.T) {
	h := newHarness(t)

	reqBody := proto.ClipboardWriteRequest{Format: "text", Text: "Hello from remote!"}
	bodyBytes, _ := json.Marshal(reqBody)

	w := h.doRequest(http.MethodPost, "/clipboard", bytes.NewReader(bodyBytes))
	if w.Code != http.StatusOK {
		t.Fatalf("write status = %d, body: %s", w.Code, w.Body.String())
	}

	var writeResp proto.ClipboardWriteResponse
	decodeResponse(t, w, &writeResp)
	if !writeResp.OK {
		t.Error("writeResp.OK = false")
	}
	if writeResp.Format != "text" {
		t.Errorf("format = %q, want %q", writeResp.Format, "text")
	}
}

func TestE2E_WritePNGClipboard(t *testing.T) {
	h := newHarness(t)

	pngData := createTestPNG(t)
	reqBody := proto.ClipboardWriteRequest{
		Format: "png",
		Image:  base64.StdEncoding.EncodeToString(pngData),
	}
	bodyBytes, _ := json.Marshal(reqBody)

	w := h.doRequest(http.MethodPost, "/clipboard", bytes.NewReader(bodyBytes))
	if w.Code != http.StatusOK {
		t.Fatalf("write status = %d, body: %s", w.Code, w.Body.String())
	}

	var writeResp proto.ClipboardWriteResponse
	decodeResponse(t, w, &writeResp)
	if !writeResp.OK {
		t.Error("writeResp.OK = false")
	}
}

func TestE2E_AuthFailure(t *testing.T) {
	h := newHarness(t)

	req := httptest.NewRequest(http.MethodGet, "/clipboard", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestE2E_MissingAuth(t *testing.T) {
	h := newHarness(t)

	req := httptest.NewRequest(http.MethodGet, "/clipboard", nil)
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestE2E_WriteUnsupportedFormat(t *testing.T) {
	h := newHarness(t)

	reqBody := proto.ClipboardWriteRequest{Format: "html", Text: "<b>bold</b>"}
	bodyBytes, _ := json.Marshal(reqBody)

	w := h.doRequest(http.MethodPost, "/clipboard", bytes.NewReader(bodyBytes))
	if w.Code == http.StatusOK {
		t.Error("expected error for unsupported format")
	}
}

func TestE2E_HistoryList(t *testing.T) {
	h := newHarness(t)

	h.reader.content = &clipboard.Content{Data: createTestPNG(t), Format: "png"}
	h.doRequest(http.MethodGet, "/clipboard", nil)

	h.reader.content = &clipboard.Content{Data: []byte("text content"), Format: "text"}
	h.doRequest(http.MethodGet, "/clipboard", nil)

	w := h.doRequest(http.MethodGet, "/clipboard/history", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("history status = %d", w.Code)
	}

	var histResp proto.HistoryResponse
	decodeResponse(t, w, &histResp)
	if !histResp.OK {
		t.Error("histResp.OK = false")
	}
	if len(histResp.Items) != 2 {
		t.Errorf("items = %d, want 2", len(histResp.Items))
	}
}

func TestE2E_AuditLogWritten(t *testing.T) {
	h := newHarness(t)

	h.reader.content = &clipboard.Content{Data: createTestPNG(t), Format: "png"}
	h.doRequest(http.MethodGet, "/clipboard", nil)

	data, err := os.ReadFile(h.cfg.AuditLog)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if len(data) == 0 {
		t.Error("audit log is empty")
	}
}

func TestE2E_RedactionBlocksSecret(t *testing.T) {
	h := newHarness(t)

	h.reader.content = &clipboard.Content{
		Data:   []byte("my AWS key is AKIAIOSFODNN7EXAMPLE"),
		Format: "text",
	}

	w := h.doRequest(http.MethodGet, "/clipboard", nil)
	if w.Code == http.StatusOK {
		t.Error("expected redaction to block content with AWS key")
	}
}

func TestE2E_RedactionAllowsCleanContent(t *testing.T) {
	h := newHarness(t)

	h.reader.content = &clipboard.Content{
		Data:   []byte("This is clean clipboard content"),
		Format: "text",
	}

	w := h.doRequest(http.MethodGet, "/clipboard", nil)
	if w.Code != http.StatusOK {
		t.Error("expected clean content to pass redaction")
	}
}

// -------------------------------------------------------------------
// Helpers
// -------------------------------------------------------------------

func createTestPNG(t *testing.T) []byte {
	t.Helper()
	png, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP4/5+hHgAHggJ/PchI7wAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatalf("create test PNG: %v", err)
	}
	return png
}
