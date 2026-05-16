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
