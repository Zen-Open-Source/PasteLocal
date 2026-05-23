package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pastelocal/pastelocal/internal/config"
)

// TestEmbedderDisabled ensures a disabled embedder is safe.
func TestEmbedderDisabled(t *testing.T) {
	cfg := &config.Config{}
	e := NewEmbedder(cfg, nil)
	if e.IsEnabled() {
		t.Fatal("expected disabled embedder")
	}
	vec, err := e.Embed(context.Background(), "hello")
	if err != nil || vec != nil {
		t.Errorf("disabled embed should return nil, nil; got %v, %v", vec, err)
	}
}

// TestParseEmbeddingVector covers the tolerant parser.
func TestParseEmbeddingVector(t *testing.T) {
	tests := []struct {
		input string
		want  []float64
	}{
		{`[0.1, 0.2, 0.3]`, []float64{0.1, 0.2, 0.3}},
		{`0.4 0.5 0.6`, []float64{0.4, 0.5, 0.6}},
		{`0.7, 0.8, 0.9`, []float64{0.7, 0.8, 0.9}},
		{`  1.0   2.0  `, []float64{1.0, 2.0}},
		{``, nil},
	}

	for _, tt := range tests {
		got, err := parseEmbeddingVector([]byte(tt.input))
		if err != nil {
			t.Errorf("parse %q error: %v", tt.input, err)
			continue
		}
		if len(got) != len(tt.want) {
			t.Errorf("parse %q len mismatch: got %d want %d", tt.input, len(got), len(tt.want))
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("parse %q mismatch at %d: got %v want %v", tt.input, i, got, tt.want)
			}
		}
	}
}

// TestEmbedderWithFakeCommand uses a real shell command that echoes a vector.
func TestEmbedderWithFakeCommand(t *testing.T) {
	// Create a temp script that reads stdin and prints a known vector.
	tmp := t.TempDir()
	script := filepath.Join(tmp, "fake_embed.sh")
	content := `#!/bin/sh
read -r line
echo '[0.1, 0.2, 0.3, 0.4]'
`
	if err := os.WriteFile(script, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Recall: config.RecallConfig{
			Enabled: true,
			Timeout: 5,
			Command: script,
		},
	}

	var logger testLogger // simple logger that discards
	e := NewEmbedder(cfg, &logger)
	if !e.IsEnabled() {
		t.Fatal("embedder should be enabled")
	}

	vec, err := e.Embed(context.Background(), "test text")
	if err != nil {
		t.Fatalf("Embed error: %v", err)
	}
	if len(vec) != 4 || vec[0] != 0.1 {
		t.Errorf("unexpected vec: %v", vec)
	}
	if e.Dim() != 4 {
		t.Errorf("dim not recorded: got %d", e.Dim())
	}
}

// testLogger is a minimal logger for tests.
type testLogger struct{}

func (l *testLogger) Error(msg string, args ...interface{}) {}
func (l *testLogger) Warn(msg string, args ...interface{})  {}
func (l *testLogger) Debug(msg string, args ...interface{}) {}

// TestEmbedderCommandFailure ensures fail-open behavior.
func TestEmbedderCommandFailure(t *testing.T) {
	cfg := &config.Config{
		Recall: config.RecallConfig{
			Enabled: true,
			Timeout: 1,
			Command: "exit 1", // will fail
		},
	}
	e := NewEmbedder(cfg, nil)
	_, err := e.Embed(context.Background(), "anything")
	if err == nil {
		t.Error("expected error from failing command")
	}
}
