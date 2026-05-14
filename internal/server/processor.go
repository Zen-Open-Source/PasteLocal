package server

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/pastelocal/pastelocal/internal/config"
	"github.com/pastelocal/pastelocal/internal/clipboard"
	cliperr "github.com/pastelocal/pastelocal/internal/errors"
)

// ProcessorPipeline runs clipboard content through a chain of external
// processor commands before it's served or written.
type ProcessorPipeline struct {
	chain   []processorStep
	timeout time.Duration
	logger  interface {
		Error(string, ...interface{})
		Warn(string, ...interface{})
	}
}

type processorStep struct {
	name    string
	command string
	on      string // "read", "write", "both"
}

// NewProcessorPipeline creates a pipeline from config.
func NewProcessorPipeline(cfg *config.Config, logger interface {
	Error(string, ...interface{})
	Warn(string, ...interface{})
}) *ProcessorPipeline {
	if !cfg.Processors.Enabled || len(cfg.Processors.Chain) == 0 {
		return &ProcessorPipeline{timeout: time.Duration(cfg.Processors.Timeout) * time.Second, logger: logger}
	}
	timeout := time.Duration(cfg.Processors.Timeout) * time.Second
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	var steps []processorStep
	for _, p := range cfg.Processors.Chain {
		steps = append(steps, processorStep{
			name:    p.Name,
			command: p.Command,
			on:      p.On,
		})
	}

	return &ProcessorPipeline{
		chain:   steps,
		timeout: timeout,
		logger:  logger,
	}
}

// ProcessRead runs the pipeline on content being read from the clipboard.
// Processors that match "read" or "both" are applied.
func (p *ProcessorPipeline) ProcessRead(ctx context.Context, content *clipboard.Content) *cliperr.Error {
	return p.process(ctx, content, "read")
}

// ProcessWrite runs the pipeline on content being written to the clipboard.
// Processors that match "write" or "both" are applied.
func (p *ProcessorPipeline) ProcessWrite(ctx context.Context, content *clipboard.Content) *cliperr.Error {
	return p.process(ctx, content, "write")
}

// process runs all matching processor steps in order.
func (p *ProcessorPipeline) process(ctx context.Context, content *clipboard.Content, direction string) *cliperr.Error {
	for _, step := range p.chain {
		if step.on != direction && step.on != "both" {
			continue
		}

		// Only process image data through external commands.
		if content.Format != "png" {
			continue
		}

		processed, err := p.runCommand(ctx, step.name, step.command, content.Data)
		if err != nil {
			p.logger.Warn("processor failed, serving unprocessed content",
				"processor", step.name, "error", err)
			// Fail-open: serve unprocessed content on processor error.
			continue
		}
		if len(processed) > 0 {
			content.Data = processed
		}
	}
	return nil
}

// runCommand executes a processor command, piping data through stdin/stdout.
func (p *ProcessorPipeline) runCommand(ctx context.Context, name, command string, data []byte) ([]byte, error) {
	procCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	cmd := exec.CommandContext(procCtx, "sh", "-c", command)
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("processor %q failed: %w: %s", name, err, stderr.String())
	}

	result := stdout.Bytes()
	if len(result) == 0 {
		return nil, fmt.Errorf("processor %q produced no output", name)
	}
	return result, nil
}

// StepCount returns the number of processor steps.
func (p *ProcessorPipeline) StepCount() int {
	return len(p.chain)
}
