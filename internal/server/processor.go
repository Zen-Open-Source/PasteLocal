package server

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/pastelocal/pastelocal/internal/clipboard"
	"github.com/pastelocal/pastelocal/internal/config"
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
			on:      "both", // direction filtering removed; all processors apply to both read/write for images
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

// --- VisionPaste analysis pipeline (v1) ---

// AnalysisResult holds text metadata extracted from an image via the vision pipeline.
type AnalysisResult struct {
	OCRText     string
	Description string
}

// AnalysisPipeline runs configured external commands to produce OCR / descriptions
// from screenshot images. Commands receive PNG bytes on stdin and must emit
// useful text on stdout. Results are best-effort; failures are logged and ignored
// (fail-open) so that clipboard flow is never blocked by analysis.
type AnalysisPipeline struct {
	chain   []analysisStep
	timeout time.Duration
	logger  interface {
		Error(string, ...interface{})
		Warn(string, ...interface{})
	}
}

type analysisStep struct {
	name    string
	command string
}

// NewAnalysisPipeline builds from config (parallel to NewProcessorPipeline).
func NewAnalysisPipeline(cfg *config.Config, logger interface {
	Error(string, ...interface{})
	Warn(string, ...interface{})
}) *AnalysisPipeline {
	if !cfg.Vision.Enabled || len(cfg.Vision.Chain) == 0 {
		return &AnalysisPipeline{timeout: time.Duration(cfg.Vision.Timeout) * time.Second, logger: logger}
	}
	timeout := time.Duration(cfg.Vision.Timeout) * time.Second
	if timeout == 0 {
		timeout = 15 * time.Second
	}

	var steps []analysisStep
	for _, v := range cfg.Vision.Chain {
		steps = append(steps, analysisStep{
			name:    v.Name,
			command: v.Command,
		})
	}
	return &AnalysisPipeline{
		chain:   steps,
		timeout: timeout,
		logger:  logger,
	}
}

// Analyze runs the analysis chain on png content and returns collected results.
// Returns nil if disabled, not an image, or no useful output produced.
// Errors from individual steps are logged as warnings and skipped (fail-open).
func (a *AnalysisPipeline) Analyze(ctx context.Context, content *clipboard.Content) *AnalysisResult {
	if len(a.chain) == 0 || content == nil || content.Format != "png" || len(content.Data) == 0 {
		return nil
	}

	res := &AnalysisResult{}
	for _, step := range a.chain {
		text, err := a.runTextCommand(ctx, step.name, step.command, content.Data)
		if err != nil {
			a.logger.Warn("analysis step failed, skipping", "step", step.name, "error", err)
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		// Map step name to result field. Unknown names go to OCRText with prefix.
		switch strings.ToLower(step.name) {
		case "ocr", "tesseract", "text", "extract":
			if res.OCRText != "" {
				res.OCRText += "\n"
			}
			res.OCRText += text
		case "describe", "caption", "summary", "vision", "alt":
			if res.Description != "" {
				res.Description += " "
			}
			res.Description += text
		default:
			if res.OCRText != "" {
				res.OCRText += "\n"
			}
			res.OCRText += step.name + ": " + text
		}
	}

	if res.OCRText == "" && res.Description == "" {
		return nil
	}
	return res
}

// runTextCommand executes a shell command with image data on stdin and returns
// the stdout as text (analysis output). Mirrors the processor runCommand pattern
// but for text-producing commands and with slightly longer default timeout.
func (a *AnalysisPipeline) runTextCommand(ctx context.Context, name, command string, data []byte) (string, error) {
	procCtx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	cmd := exec.CommandContext(procCtx, "sh", "-c", command)
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("analysis %q failed: %w: %s", name, err, stderr.String())
	}

	result := strings.TrimSpace(stdout.String())
	if result == "" {
		return "", fmt.Errorf("analysis %q produced no output", name)
	}
	return result, nil
}

// AnalysisStepCount returns how many analysis steps are configured.
func (a *AnalysisPipeline) AnalysisStepCount() int {
	return len(a.chain)
}
