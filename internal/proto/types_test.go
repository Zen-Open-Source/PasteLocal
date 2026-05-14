package proto

import (
	"encoding/json"
	"testing"
)

func TestClipboardResponseSerialization(t *testing.T) {
	orig := ClipboardResponse{
		OK:         true,
		Image:      "iVBORw0KGgoAAAANSUhEUg==",
		Format:     "png",
		ByteCount:  2048,
		CapturedAt: "2025-05-11T12:00:00Z",
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal ClipboardResponse: %v", err)
	}

	var got ClipboardResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal ClipboardResponse: %v", err)
	}

	if got.OK != orig.OK {
		t.Errorf("OK = %v, want %v", got.OK, orig.OK)
	}
	if got.Image != orig.Image {
		t.Errorf("Image = %q, want %q", got.Image, orig.Image)
	}
	if got.Format != orig.Format {
		t.Errorf("Format = %q, want %q", got.Format, orig.Format)
	}
	if got.ByteCount != orig.ByteCount {
		t.Errorf("ByteCount = %d, want %d", got.ByteCount, orig.ByteCount)
	}
	if got.CapturedAt != orig.CapturedAt {
		t.Errorf("CapturedAt = %q, want %q", got.CapturedAt, orig.CapturedAt)
	}
}

func TestClipboardResponseBase64RoundTrip(t *testing.T) {
	// Simulate a real base64-encoded PNG payload round-tripping through JSON.
	base64PNG := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/PchI7wAAAABJRU5ErkJggg=="

	orig := ClipboardResponse{
		OK:         true,
		Image:      base64PNG,
		Format:     "png",
		ByteCount:  67,
		CapturedAt: "2025-05-11T12:00:00Z",
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got ClipboardResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Image != base64PNG {
		t.Errorf("base64 image did not round-trip: got %q, want %q", got.Image, base64PNG)
	}
}

func TestClipboardResponseJSONKeys(t *testing.T) {
	resp := ClipboardResponse{
		OK:         true,
		Image:      "abc",
		Format:     "png",
		ByteCount:  100,
		CapturedAt: "2025-05-11T12:00:00Z",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}

	expectedKeys := []string{"ok", "image", "format", "byte_count", "captured_at"}
	for _, key := range expectedKeys {
		if _, ok := m[key]; !ok {
			t.Errorf("missing JSON key %q in output: %v", key, string(data))
		}
	}
}

func TestErrorResponseSerialization(t *testing.T) {
	orig := ErrorResponse{
		OK:      false,
		Code:    "E_NO_PEER",
		Error:   "no peer connected",
		FixHint: "run 'pastelocal pair'",
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal ErrorResponse: %v", err)
	}

	var got ErrorResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal ErrorResponse: %v", err)
	}

	if got.OK != orig.OK {
		t.Errorf("OK = %v, want %v", got.OK, orig.OK)
	}
	if got.Code != orig.Code {
		t.Errorf("Code = %q, want %q", got.Code, orig.Code)
	}
	if got.Error != orig.Error {
		t.Errorf("Error = %q, want %q", got.Error, orig.Error)
	}
	if got.FixHint != orig.FixHint {
		t.Errorf("FixHint = %q, want %q", got.FixHint, orig.FixHint)
	}
}

func TestErrorResponseJSONKeys(t *testing.T) {
	resp := ErrorResponse{
		OK:      false,
		Code:    "E_FAIL",
		Error:   "something went wrong",
		FixHint: "try again",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}

	expectedKeys := []string{"ok", "code", "error", "fix_hint"}
	for _, key := range expectedKeys {
		if _, ok := m[key]; !ok {
			t.Errorf("missing JSON key %q in output: %v", key, string(data))
		}
	}
}

func TestVersionResponseSerialization(t *testing.T) {
	orig := VersionResponse{
		OK:              true,
		ProtocolVersion: ProtocolVersion,
		BinaryVersion:   "0.1.0",
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal VersionResponse: %v", err)
	}

	var got VersionResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal VersionResponse: %v", err)
	}

	if got.OK != orig.OK {
		t.Errorf("OK = %v, want %v", got.OK, orig.OK)
	}
	if got.ProtocolVersion != orig.ProtocolVersion {
		t.Errorf("ProtocolVersion = %d, want %d", got.ProtocolVersion, orig.ProtocolVersion)
	}
	if got.BinaryVersion != orig.BinaryVersion {
		t.Errorf("BinaryVersion = %q, want %q", got.BinaryVersion, orig.BinaryVersion)
	}
}

func TestVersionResponseJSONKeys(t *testing.T) {
	resp := VersionResponse{
		OK:              true,
		ProtocolVersion: 1,
		BinaryVersion:   "0.1.0",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}

	expectedKeys := []string{"ok", "protocol_version", "binary_version"}
	for _, key := range expectedKeys {
		if _, ok := m[key]; !ok {
			t.Errorf("missing JSON key %q in output: %v", key, string(data))
		}
	}
}

func TestHealthResponseSerialization(t *testing.T) {
	orig := HealthResponse{OK: true}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal HealthResponse: %v", err)
	}

	var got HealthResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal HealthResponse: %v", err)
	}

	if got.OK != orig.OK {
		t.Errorf("OK = %v, want %v", got.OK, orig.OK)
	}
}

func TestProtocolVersionConstant(t *testing.T) {
	if ProtocolVersion != 2 {
		t.Errorf("ProtocolVersion = %d, want 2", ProtocolVersion)
	}
}
