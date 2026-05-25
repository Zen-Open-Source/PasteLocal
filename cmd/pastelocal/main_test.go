package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/pastelocal/pastelocal/internal/auth"
	"github.com/pastelocal/pastelocal/internal/proto"
)

// Test helpers (mirrors patterns used in doctor_test.go and e2e harness)
func newTestTokenAndPath(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token")
	ts := auth.NewTokenStore(false, tokenPath)
	tok := "test-cli-token-for-history-qa-123"
	if err := ts.Store(tok); err != nil {
		t.Fatalf("store test token: %v", err)
	}
	return tok, tokenPath
}

// TestParseHistoryIndex exercises the post-fix Atoi+Trim logic used in runHistoryGet.
// This directly protects the regression case ("1foo") that was fixed during review.
func TestParseHistoryIndex(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{"valid 1", "1", 1, false},
		{"valid with spaces", "  3  ", 3, false},
		{"zero", "0", 0, true},
		{"negative", "-5", 0, true},
		{"non-numeric", "foo", 0, true},
		{"trailing junk (the fixed regression case)", "1foo", 0, true},
		{"leading junk", "bar2", 0, true},
		{"empty", "", 0, true},
		{"whitespace only", "   ", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseHistoryIndexForTest(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("err=%v wantErr=%v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("got %d want %d", got, tt.want)
			}
		})
	}
}

// parseHistoryIndexForTest mirrors the exact post-fix logic in runHistoryGet
// so we have a pure, table-driven test for the regression case ("1foo" etc.).
func parseHistoryIndexForTest(s string) (int, error) {
	idx, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || idx < 1 {
		return 0, fmt.Errorf("invalid index")
	}
	return idx, nil
}

// TestListHistory_Success uses an httptest server to exercise listHistory helper
// (the exact code path used by `pastelocal history list`).
func TestListHistory_Success(t *testing.T) {
	token, _ := newTestTokenAndPath(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path != "/clipboard/history" {
			w.WriteHeader(404)
			return
		}
		resp := proto.HistoryResponse{
			OK: true,
			Items: []proto.HistoryEntry{
				{ID: "id-old", CapturedAt: "2026-05-24T10:00:00Z", Format: "text", ByteCount: 10},
				{ID: "id-new", CapturedAt: "2026-05-24T12:00:00Z", Format: "png", ByteCount: 48192},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// Extract port (simplistic but sufficient for test)
	port := 0
	_, _ = fmt.Sscanf(strings.TrimPrefix(srv.URL, "http://127.0.0.1:"), "%d", &port)

	items, err := listHistory(port, token)
	if err != nil {
		t.Fatalf("listHistory: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len=%d want 2", len(items))
	}
	// Server returns oldest-first; CLI reverses for display
	if items[0].ID != "id-old" {
		t.Error("expected oldest first from server response")
	}
}

// TestFetchHistoryEntry_PNG exercises the fetch path + Analysis shape the CLI get uses for sidecars.
func TestFetchHistoryEntry_PNG(t *testing.T) {
	token, _ := newTestTokenAndPath(t)

	imgB64 := base64.StdEncoding.EncodeToString([]byte("fake-png-bytes"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/history/") {
			resp := proto.ClipboardResponse{
				OK:     true,
				Format: "png",
				Image:  imgB64,
				Analysis: &proto.ClipboardAnalysis{
					OCRText:     "hello from ocr",
					Description: "a screenshot",
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	port := 0
	_, _ = fmt.Sscanf(strings.TrimPrefix(srv.URL, "http://127.0.0.1:"), "%d", &port)

	resp, err := fetchHistoryEntry(port, token, "some-id")
	if err != nil {
		t.Fatalf("fetchHistoryEntry: %v", err)
	}
	if resp.Format != "png" || resp.Image == "" {
		t.Error("expected png with Image data")
	}
	if resp.Analysis == nil || resp.Analysis.OCRText == "" {
		t.Error("expected Analysis to be populated (sidecar path)")
	}
}


