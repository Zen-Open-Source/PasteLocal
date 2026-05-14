package server

import (
	"testing"

	"github.com/pastelocal/pastelocal/internal/config"
)

func TestRedactionEngineBlockAWSKey(t *testing.T) {
	cfg := config.Default()
	cfg.Redaction.Enabled = true
	e := NewRedactionEngine(cfg)

	text := "my key is AKIAIOSFODNN7EXAMPLE"
	err := e.CheckText(&text)
	if err == nil {
		t.Error("expected redaction error for AWS key")
	}
	if err.Code != "CB1010" {
		t.Errorf("error code = %q, want %q", err.Code, "CB1010")
	}
}

func TestRedactionEngineBlockGitHubToken(t *testing.T) {
	cfg := config.Default()
	cfg.Redaction.Enabled = true
	e := NewRedactionEngine(cfg)

	text := "token is ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijkl"
	err := e.CheckText(&text)
	if err == nil {
		t.Error("expected redaction error for GitHub token")
	}
	if err.Code != "CB1010" {
		t.Errorf("error code = %q, want %q", err.Code, "CB1010")
	}
}

func TestRedactionEngineBlockPrivateKey(t *testing.T) {
	cfg := config.Default()
	cfg.Redaction.Enabled = true
	e := NewRedactionEngine(cfg)

	text := "-----BEGIN RSA PRIVATE KEY-----\nMIIEowI..."
	err := e.CheckText(&text)
	if err == nil {
		t.Error("expected redaction error for private key")
	}
	if err.Code != "CB1010" {
		t.Errorf("error code = %q, want %q", err.Code, "CB1010")
	}
}

func TestRedactionEnginePassesCleanContent(t *testing.T) {
	cfg := config.Default()
	cfg.Redaction.Enabled = true
	e := NewRedactionEngine(cfg)

	text := "Hello, this is a normal clipboard content"
	err := e.CheckText(&text)
	if err != nil {
		t.Errorf("unexpected error for clean content: %v", err)
	}
	if text != "Hello, this is a normal clipboard content" {
		t.Errorf("text was modified unexpectedly: %q", text)
	}
}

func TestRedactionEngineRedactAction(t *testing.T) {
	cfg := config.Default()
	cfg.Redaction.Enabled = true
	cfg.Redaction.Rules = []config.RedactionRule{
		{
			Name:    "test-redact",
			Pattern: `secret-\d+`,
			Action:  "redact",
		},
	}
	e := NewRedactionEngine(cfg)

	text := "the code is secret-12345 here"
	err := e.CheckText(&text)
	if err != nil {
		t.Errorf("unexpected error for redact action: %v", err)
	}
	if text != "the code is ***REDACTED*** here" {
		t.Errorf("text = %q, want redacted", text)
	}
}

func TestRedactionEngineDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.Redaction.Enabled = false
	e := NewRedactionEngine(cfg)

	text := "AKIAIOSFODNN7EXAMPLE"
	err := e.CheckText(&text)
	if err != nil {
		t.Errorf("unexpected error when redaction disabled: %v", err)
	}
}

func TestRedactionEngineImageData(t *testing.T) {
	cfg := config.Default()
	cfg.Redaction.Enabled = true
	e := NewRedactionEngine(cfg)

	// Image data with an embedded AWS key pattern in ASCII.
	data := []byte{0x89, 0x50, 0x4E, 0x47} // PNG header
	data = append(data, []byte("  AKIAIOSFODNN7EXAMPLE  ")...)
	data = append(data, 0x00)

	err := e.CheckImageData(data)
	if err == nil {
		t.Error("expected redaction error for image data with embedded secret")
	}
}

func TestRedactionEngineCleanImageData(t *testing.T) {
	cfg := config.Default()
	cfg.Redaction.Enabled = true
	e := NewRedactionEngine(cfg)

	data := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	err := e.CheckImageData(data)
	if err != nil {
		t.Errorf("unexpected error for clean image data: %v", err)
	}
}

func TestRedactionEngineRuleCount(t *testing.T) {
	cfg := config.Default()
	cfg.Redaction.Enabled = true
	e := NewRedactionEngine(cfg)

	// Should have the 5 default rules.
	if e.RuleCount() != 5 {
		t.Errorf("RuleCount() = %d, want 5", e.RuleCount())
	}
}

func TestRedactionEngineInvalidPattern(t *testing.T) {
	cfg := config.Default()
	cfg.Redaction.Enabled = true
	cfg.Redaction.Rules = []config.RedactionRule{
		{
			Name:    "invalid",
			Pattern: `(?P<invalid`, // invalid regex
			Action:  "block",
		},
		{
			Name:    "valid",
			Pattern: `test-\d+`,
			Action:  "block",
		},
	}
	e := NewRedactionEngine(cfg)

	// Invalid pattern should be skipped, only valid one counted.
	if e.RuleCount() != 1 {
		t.Errorf("RuleCount() = %d, want 1", e.RuleCount())
	}
}
