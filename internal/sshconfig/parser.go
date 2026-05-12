// Package sshconfig parses and edits OpenSSH config files while
// preserving comments, blank lines, and whitespace formatting.
package sshconfig

import (
	"strings"
)

// Block represents a Host or Match block in ssh config.
type Block struct {
	Name   string // the pattern after Host or Match keyword
	IsHost bool   // true for Host, false for Match
	Lines  []Line // lines within the block (including Host/Match line)
}

// Line represents a single line in ssh config.
type Line struct {
	Raw       string // original line text (including whitespace, comments)
	Keyword   string // e.g., "Host", "HostName", "RemoteForward"
	Argument  string // the rest after keyword
	IsComment bool
	IsBlank   bool
	Indent    string // leading whitespace
}

// Config represents a parsed ssh config file.
type Config struct {
	Lines  []Line // all lines in order (including between blocks)
	Blocks []Block // parsed blocks
}

// Parse parses ssh config content. It preserves comments, blank lines,
// whitespace/indentation, order of lines, and lines outside any
// Host/Match block (global lines).
func Parse(content string) (*Config, error) {
	cfg := &Config{}

	if content == "" {
		return cfg, nil
	}

	rawLines := strings.Split(content, "\n")

	for _, raw := range rawLines {
		line := parseLine(raw)
		cfg.Lines = append(cfg.Lines, line)
	}

	// Build blocks from parsed lines.
	cfg.Blocks = buildBlocks(cfg.Lines)

	return cfg, nil
}

// parseLine parses a single raw line into a Line struct.
func parseLine(raw string) Line {
	l := Line{Raw: raw}

	trimmed := strings.TrimRight(raw, " \t")
	if trimmed == "" {
		l.IsBlank = true
		return l
	}

	// Determine indent (leading whitespace).
	indentEnd := 0
	for _, ch := range raw {
		if ch == ' ' || ch == '\t' {
			indentEnd++
		} else {
			break
		}
	}
	l.Indent = raw[:indentEnd]

	// Strip leading whitespace for keyword/argument parsing.
	rest := raw[indentEnd:]

	// Check for comment lines.
	if strings.HasPrefix(rest, "#") {
		l.IsComment = true
		return l
	}

	// Split into keyword and argument.
	// SSH config is case-insensitive for keywords, but we preserve
	// the original casing in Raw and store keyword in its original form.
	spaceIdx := strings.IndexAny(rest, " \t")
	if spaceIdx == -1 {
		l.Keyword = rest
		l.Argument = ""
	} else {
		l.Keyword = rest[:spaceIdx]
		l.Argument = strings.TrimLeft(rest[spaceIdx:], " \t")
	}

	return l
}

// buildBlocks groups lines into Host/Match blocks. Lines before any
// Host/Match block are treated as global lines and are not part of any Block.
func buildBlocks(lines []Line) []Block {
	var blocks []Block
	var current *Block

	for i := range lines {
		l := &lines[i]

		if l.IsBlank || l.IsComment {
			if current != nil {
				current.Lines = append(current.Lines, *l)
			}
			continue
		}

		kw := strings.ToLower(l.Keyword)
		if kw == "host" || kw == "match" {
			// Start a new block.
			if current != nil {
				blocks = append(blocks, *current)
			}
			current = &Block{
				Name:   l.Argument,
				IsHost: kw == "host",
				Lines:  []Line{*l},
			}
			continue
		}

		// Regular keyword line.
		if current != nil {
			current.Lines = append(current.Lines, *l)
		}
	}

	if current != nil {
		blocks = append(blocks, *current)
	}

	return blocks
}

// Render renders the config back to a string. It reproduces the original
// formatting by using the Raw field of each line.
func (c *Config) Render() string {
	if len(c.Lines) == 0 {
		return ""
	}

	var sb strings.Builder
	for i, l := range c.Lines {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(l.Raw)
	}
	return sb.String()
}

// lineIndex returns the index in cfg.Lines of the first line that has
// the given keyword and argument (case-insensitive keyword match).
// Returns -1 if not found.
func lineIndex(lines []Line, keyword, argument string) int {
	for i, l := range lines {
		if strings.ToLower(l.Keyword) == strings.ToLower(keyword) && l.Argument == argument {
			return i
		}
	}
	return -1
}

// blockStartLine returns the index in cfg.Lines of the first line
// belonging to the given block.
func blockStartLine(cfg *Config, blk *Block) int {
	if len(blk.Lines) == 0 {
		return -1
	}
	target := blk.Lines[0].Raw
	for i, l := range cfg.Lines {
		if l.Raw == target {
			return i
		}
	}
	return -1
}

// blockEndLine returns the index in cfg.Lines of the last line
// belonging to the given block.
func blockEndLine(cfg *Config, blk *Block) int {
	if len(blk.Lines) == 0 {
		return -1
	}
	target := blk.Lines[len(blk.Lines)-1].Raw
	// Search backwards for the last line to handle duplicate raw strings.
	for i := len(cfg.Lines) - 1; i >= 0; i-- {
		if cfg.Lines[i].Raw == target {
			return i
		}
	}
	return -1
}

// isBlockHeader returns whether a line is a Host or Match keyword.
func isBlockHeader(l Line) bool {
	kw := strings.ToLower(l.Keyword)
	return kw == "host" || kw == "match"
}
