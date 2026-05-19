package server

import (
	"context"
	"testing"

	"github.com/pastelocal/pastelocal/internal/clipboard"
	"github.com/pastelocal/pastelocal/internal/config"
)

type mockLogger struct{}

func (m *mockLogger) Error(msg string, args ...interface{}) {}
func (m *mockLogger) Warn(msg string, args ...interface{})  {}

func TestProcessorPipelineDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.Processors.Enabled = false
	e := NewProcessorPipeline(cfg, &mockLogger{})

	if e.StepCount() != 0 {
		t.Errorf("StepCount() = %d, want 0", e.StepCount())
	}

	content := &clipboard.Content{Data: []byte("test"), Format: "png"}
	err := e.ProcessRead(context.Background(), content)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if string(content.Data) != "test" {
		t.Errorf("content was modified unexpectedly")
	}
}

func TestProcessorPipelinePassesTextThrough(t *testing.T) {
	cfg := config.Default()
	cfg.Processors.Enabled = true
	cfg.Processors.Chain = []config.ProcessorEntry{
		{Name: "test", Command: "cat"},
	}
	e := NewProcessorPipeline(cfg, &mockLogger{})

	content := &clipboard.Content{Data: []byte("text content"), Format: "text"}
	err := e.ProcessRead(context.Background(), content)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	// Text should pass through unchanged (processors only run on png).
	if string(content.Data) != "text content" {
		t.Errorf("content = %q, want %q", string(content.Data), "text content")
	}
}

func TestProcessorPipelineRunsOnPng(t *testing.T) {
	cfg := config.Default()
	cfg.Processors.Enabled = true
	cfg.Processors.Chain = []config.ProcessorEntry{
		{Name: "echo-test", Command: "echo processed"},
	}
	e := NewProcessorPipeline(cfg, &mockLogger{})

	if e.StepCount() != 1 {
		t.Errorf("StepCount() = %d, want 1", e.StepCount())
	}

	content := &clipboard.Content{Data: []byte("original-png"), Format: "png"}
	err := e.ProcessRead(context.Background(), content)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	// The echo command should replace the data.
	if string(content.Data) == "original-png" {
		t.Error("content was not processed")
	}
}

func TestProcessorPipelineFailOpen(t *testing.T) {
	cfg := config.Default()
	cfg.Processors.Enabled = true
	cfg.Processors.Chain = []config.ProcessorEntry{
		{Name: "failing", Command: "exit 1"},
	}
	e := NewProcessorPipeline(cfg, &mockLogger{})

	content := &clipboard.Content{Data: []byte("original-data"), Format: "png"}
	err := e.ProcessRead(context.Background(), content)
	// Should not return error (fail-open).
	if err != nil {
		t.Errorf("expected fail-open, got error: %v", err)
	}
	// Original data should be preserved.
	if string(content.Data) != "original-data" {
		t.Errorf("content = %q, want %q", string(content.Data), "original-data")
	}
}

func TestProcessorPipelineWriteDirection(t *testing.T) {
	cfg := config.Default()
	cfg.Processors.Enabled = true
	cfg.Processors.Chain = []config.ProcessorEntry{
		{Name: "read-only", Command: "echo read"},
		{Name: "write-only", Command: "echo write"},
	}
	e := NewProcessorPipeline(cfg, &mockLogger{})

	content := &clipboard.Content{Data: []byte("data"), Format: "png"}

	// ProcessRead should only run the "read" step.
	err := e.ProcessRead(context.Background(), content)
	if err != nil {
		t.Errorf("ProcessRead error: %v", err)
	}

	// ProcessWrite should only run the "write" step.
	content2 := &clipboard.Content{Data: []byte("data"), Format: "png"}
	err = e.ProcessWrite(context.Background(), content2)
	if err != nil {
		t.Errorf("ProcessWrite error: %v", err)
	}
}

// --- AnalysisPipeline (VisionPaste) tests ---

func TestAnalysisPipelineDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.Vision.Enabled = false
	a := NewAnalysisPipeline(cfg, &mockLogger{})

	if a.AnalysisStepCount() != 0 {
		t.Errorf("AnalysisStepCount() = %d, want 0", a.AnalysisStepCount())
	}

	content := &clipboard.Content{Data: []byte("fake-png"), Format: "png"}
	res := a.Analyze(context.Background(), content)
	if res != nil {
		t.Errorf("expected nil result when disabled, got %+v", res)
	}
}

func TestAnalysisPipelineNoOutputForText(t *testing.T) {
	cfg := config.Default()
	cfg.Vision.Enabled = true
	cfg.Vision.Chain = []config.VisionEntry{{Name: "ocr", Command: "echo ocr-text"}}
	a := NewAnalysisPipeline(cfg, &mockLogger{})

	content := &clipboard.Content{Data: []byte("some text"), Format: "text"}
	res := a.Analyze(context.Background(), content)
	if res != nil {
		t.Errorf("expected nil for non-png, got %+v", res)
	}
}

func TestAnalysisPipelineRunsOnPng(t *testing.T) {
	cfg := config.Default()
	cfg.Vision.Enabled = true
	cfg.Vision.Chain = []config.VisionEntry{
		{Name: "ocr", Command: "printf 'line1\nline2'"},
		{Name: "describe", Command: "echo 'a ui screenshot'"},
	}
	a := NewAnalysisPipeline(cfg, &mockLogger{})

	content := &clipboard.Content{Data: []byte{0x89, 0x50, 0x4e, 0x47}, Format: "png"}
	res := a.Analyze(context.Background(), content)
	if res == nil {
		t.Fatalf("expected analysis result")
	}
	if res.OCRText != "line1\nline2" {
		t.Errorf("OCRText = %q, want %q", res.OCRText, "line1\nline2")
	}
	if res.Description != "a ui screenshot" {
		t.Errorf("Description = %q, want %q", res.Description, "a ui screenshot")
	}
}

func TestAnalysisPipelineFailOpen(t *testing.T) {
	cfg := config.Default()
	cfg.Vision.Enabled = true
	cfg.Vision.Chain = []config.VisionEntry{
		{Name: "ocr", Command: "false"}, // fails
		{Name: "describe", Command: "echo 'still works'"},
	}
	a := NewAnalysisPipeline(cfg, &mockLogger{})

	content := &clipboard.Content{Data: []byte("pngdata"), Format: "png"}
	res := a.Analyze(context.Background(), content)
	if res == nil {
		t.Fatal("expected partial result (fail-open on first step)")
	}
	if res.Description != "still works" {
		t.Errorf("Description = %q, want 'still works'", res.Description)
	}
	if res.OCRText != "" {
		t.Errorf("OCRText should be empty on failed step, got %q", res.OCRText)
	}
}
