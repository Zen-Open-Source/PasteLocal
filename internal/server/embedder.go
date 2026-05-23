package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pastelocal/pastelocal/internal/config"
)

// Embedder runs a user-supplied external command to produce dense vector embeddings
// for text (plain clipboard text or VisionPaste OCR+description for images).
// It is the core of Recall v2 semantic history search.
//
// Design mirrors AnalysisPipeline for consistency:
// - Single configured command (not a chain; one embedding model).
// - Stdin: the search text (UTF-8).
// - Stdout: embedding vector as whitespace-separated floats or JSON array of numbers.
// - Best-effort / fail-open: errors are logged and result in no embedding (entry still
//   retrievable via --list / --id, just not via --search).
// - Never called for concealed items (enforced by callers).
// - Timeout + context cancellation.
// - Tracks embedding dimension on first success for doctor / status reporting.
type Embedder struct {
	enabled bool
	command string
	timeout time.Duration
	logger  interface {
		Error(string, ...interface{})
		Warn(string, ...interface{})
		Debug(string, ...interface{})
	}
	lastDim atomic.Int64 // dimension of last successful embedding (0 if none) - accessed atomically
}

// NewEmbedder builds an Embedder from config. If disabled or no command, returns a
// disabled instance (IsEnabled() == false) that is safe to call.
// The logger may be nil; a no-op logger is used in that case (important for tests).
func NewEmbedder(cfg *config.Config, logger interface {
	Error(string, ...interface{})
	Warn(string, ...interface{})
	Debug(string, ...interface{})
}) *Embedder {
	if logger == nil {
		logger = &noopLogger{}
	}
	if !cfg.Recall.Enabled || strings.TrimSpace(cfg.Recall.Command) == "" {
		return &Embedder{enabled: false, logger: logger}
	}
	timeout := time.Duration(cfg.Recall.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &Embedder{
		enabled: true,
		command: strings.TrimSpace(cfg.Recall.Command),
		timeout: timeout,
		logger:  logger,
	}
}

// noopLogger is a safe no-op implementation used when a nil logger is passed
// (common in unit tests).
type noopLogger struct{}

func (n *noopLogger) Error(string, ...interface{}) {}
func (n *noopLogger) Warn(string, ...interface{})  {}
func (n *noopLogger) Debug(string, ...interface{}) {}

// IsEnabled reports whether recall is configured and ready to embed text.
func (e *Embedder) IsEnabled() bool {
	return e != nil && e.enabled
}

// Dim returns the embedding dimension recorded from the last successful Embed call
// (0 if no successful call has occurred yet). Useful for doctor checks and TUI.
func (e *Embedder) Dim() int {
	if e == nil {
		return 0
	}
	return int(e.lastDim.Load())
}

// Embed runs the configured external command to embed the given text.
// Returns the vector and nil on success. On any failure (timeout, non-zero exit,
// unparsable output, empty result) it returns nil, err (caller should treat as
// "no embedding available for this item" — fail-open).
func (e *Embedder) Embed(ctx context.Context, text string) ([]float64, error) {
	if !e.enabled || e.command == "" || strings.TrimSpace(text) == "" {
		return nil, nil
	}

	procCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	cmd := exec.CommandContext(procCtx, "sh", "-c", e.command)
	cmd.Stdin = bytes.NewReader([]byte(text))

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		e.logger.Warn("recall embed command failed", "command", e.command, "error", err, "stderr", strings.TrimSpace(stderr.String()))
		return nil, fmt.Errorf("embed command failed: %w", err)
	}

	vec, err := parseEmbeddingVector(stdout.Bytes())
	if err != nil {
		e.logger.Warn("recall embed output unparsable", "command", e.command, "error", err)
		return nil, err
	}
	if len(vec) == 0 {
		return nil, fmt.Errorf("embed command produced empty vector")
	}

	// Record dimension for status/doctor (first success wins; later calls assumed consistent).
	// Use atomic operations because Dim() can be called concurrently from /version handler
	// while Embed() is called from read paths and the watcher.
	dim := int64(len(vec))
	prev := e.lastDim.Load()
	if prev == 0 {
		e.lastDim.Store(dim)
	} else if prev != dim {
		e.logger.Warn("recall embedding dimension changed", "previous", prev, "current", dim)
		e.lastDim.Store(dim)
	}

	return vec, nil
}

// parseEmbeddingVector accepts either a JSON array of numbers or whitespace/comma-
// separated floats. It is tolerant of extra whitespace and trailing commas.
func parseEmbeddingVector(b []byte) ([]float64, error) {
	s := strings.TrimSpace(string(b))
	if s == "" {
		return nil, nil
	}

	// Try JSON array first (most robust for Python/JS embedders).
	var arr []float64
	if err := json.Unmarshal([]byte(s), &arr); err == nil {
		return arr, nil
	}

	// Fallback: whitespace or comma separated tokens.
	tokens := strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == ','
	})

	var out []float64
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		f, err := strconv.ParseFloat(t, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid float %q in embedding output", t)
		}
		out = append(out, f)
	}
	return out, nil
}

// TestCommand is a helper for doctor checks: runs the embed command with a tiny
// sample text and returns (dim, error). Does not mutate lastDim.
func (e *Embedder) TestCommand(ctx context.Context, sample string) (int, error) {
	if !e.enabled || e.command == "" {
		return 0, fmt.Errorf("recall not enabled or no command configured")
	}
	// Use a short timeout for the doctor probe.
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, "sh", "-c", e.command)
	cmd.Stdin = bytes.NewReader([]byte(sample))

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("test embed failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	vec, err := parseEmbeddingVector(stdout.Bytes())
	if err != nil {
		return 0, err
	}
	if len(vec) == 0 {
		return 0, fmt.Errorf("test embed produced empty vector")
	}
	return len(vec), nil
}
