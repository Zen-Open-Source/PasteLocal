package server

import (
	"regexp"
	"strings"

	"github.com/pastelocal/pastelocal/internal/config"
	cliperr "github.com/pastelocal/pastelocal/internal/errors"
)

// RedactionEngine checks clipboard content against configured redaction rules.
type RedactionEngine struct {
	rules []compiledRule
}

type compiledRule struct {
	name   string
	action string // "redact" or "block"
	re     *regexp.Regexp
	desc   string
}

// NewRedactionEngine creates a new engine from the config.
func NewRedactionEngine(cfg *config.Config) *RedactionEngine {
	e := &RedactionEngine{}
	if !cfg.Redaction.Enabled {
		return e
	}
	for _, r := range cfg.Redaction.Rules {
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			continue // skip invalid patterns
		}
		e.rules = append(e.rules, compiledRule{
			name:   r.Name,
			action: r.Action,
			re:     re,
			desc:   r.Description,
		})
	}
	return e
}

// CheckText checks plain text content against redaction rules.
// Returns nil if the content passes, or an error if it should be blocked.
// The content pointer may be modified if redaction rules apply.
func (e *RedactionEngine) CheckText(content *string) *cliperr.Error {
	if len(e.rules) == 0 || content == nil {
		return nil
	}
	text := *content
	for _, r := range e.rules {
		if r.re.MatchString(text) {
			if r.action == "block" {
				return cliperr.NewWithMessage("CB1010",
					"Content blocked by rule: "+r.name+" ("+r.desc+")")
			}
			// redact: replace matches with ***REDACTED***
			text = r.re.ReplaceAllString(text, "***REDACTED***")
		}
	}
	*content = text
	return nil
}

// CheckImageData checks if image data might contain embedded text that
// matches redaction rules. Since we can't OCR in-process, we check
// for text-like patterns in the raw bytes as a best-effort heuristic.
// For images, we only apply block rules (not redaction, since we can't
// modify image content).
func (e *RedactionEngine) CheckImageData(data []byte) *cliperr.Error {
	if len(e.rules) == 0 {
		return nil
	}
	// Quick heuristic: scan for ASCII strings in the image data
	// and check those against text rules. This catches cases where
	// someone copies text that gets rendered as an image.
	chunks := extractASCIIChunks(data, 20)
	for _, chunk := range chunks {
		for _, r := range e.rules {
			if r.action == "block" && r.re.MatchString(chunk) {
				return cliperr.NewWithMessage("CB1010",
					"Content blocked by rule: "+r.name+" ("+r.desc+")")
			}
		}
	}
	return nil
}

// RuleCount returns the number of active rules.
func (e *RedactionEngine) RuleCount() int {
	return len(e.rules)
}

// extractASCIIChunks extracts printable ASCII runs of at least minLen
// from raw bytes. Used as a heuristic for scanning image data.
func extractASCIIChunks(data []byte, minLen int) []string {
	var chunks []string
	var current strings.Builder
	for _, b := range data {
		if b >= 32 && b < 127 {
			current.WriteByte(b)
		} else {
			if current.Len() >= minLen {
				chunks = append(chunks, current.String())
			}
			current.Reset()
		}
	}
	if current.Len() >= minLen {
		chunks = append(chunks, current.String())
	}
	return chunks
}
