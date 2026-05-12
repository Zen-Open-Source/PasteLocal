package sshconfig

import (
	"fmt"
	"strings"
)

// FindBlock finds a block by name. Returns nil if not found.
func FindBlock(cfg *Config, name string) *Block {
	for i := range cfg.Blocks {
		if cfg.Blocks[i].Name == name {
			return &cfg.Blocks[i]
		}
	}
	return nil
}

// AddRemoteForward adds a RemoteForward line to the specified host block.
// If the block doesn't exist, it appends a new block at the end.
// Comment marker: "# pastelocal:<alias>:remoteforward"
// Returns an error if a conflicting RemoteForward for the same port already exists.
func AddRemoteForward(cfg *Config, alias string, port int) error {
	blk := FindBlock(cfg, alias)
	arg := fmt.Sprintf("%d 127.0.0.1:%d", port, port)
	marker := fmt.Sprintf("# pastelocal:%s:remoteforward", alias)

	if blk != nil {
		// Check existing RemoteForward lines in the block.
		for _, l := range blk.Lines {
			if strings.ToLower(l.Keyword) != "remoteforward" {
				continue
			}
			if isSamePort(l.Argument, port) {
				// Check if it has our marker → no-op.
				if strings.Contains(l.Raw, marker) {
					return nil
				}
				// Different RemoteForward for same port → conflict.
				return fmt.Errorf("conflicting RemoteForward for port %d already exists in Host %s", port, alias)
			}
		}

		// Determine indentation to match block style.
		indent := blockIndent(blk)

		// Build the new line.
		raw := fmt.Sprintf("%sRemoteForward %s  %s", indent, arg, marker)
		newLine := Line{
			Raw:      raw,
			Keyword:  "RemoteForward",
			Argument: arg,
			Indent:   indent,
		}

		// Find where to insert: after the last non-blank, non-comment line
		// in the block, but before the next Host/Match block.
		insertAt := findInsertIndex(cfg, blk)

		// Insert into cfg.Lines.
		cfg.Lines = append(cfg.Lines, Line{})
		copy(cfg.Lines[insertAt+1:], cfg.Lines[insertAt:])
		cfg.Lines[insertAt] = newLine

		// Also add to block's Lines.
		blk.Lines = append(blk.Lines, newLine)

		// Rebuild blocks since line indices shifted.
		cfg.Blocks = buildBlocks(cfg.Lines)

		return nil
	}

	// Block doesn't exist — create a new one.
	indent := "  " // default indent for new blocks
	raw := fmt.Sprintf("%sRemoteForward %s  %s", indent, arg, marker)
	rfLine := Line{
		Raw:      raw,
		Keyword:  "RemoteForward",
		Argument: arg,
		Indent:   indent,
	}
	hostLine := Line{
		Raw:      fmt.Sprintf("Host %s", alias),
		Keyword:  "Host",
		Argument: alias,
		Indent:   "",
	}

	// Ensure a blank line before the new block if there are existing lines
	// and the last line is not blank.
	if len(cfg.Lines) > 0 {
		last := cfg.Lines[len(cfg.Lines)-1]
		if !last.IsBlank {
			cfg.Lines = append(cfg.Lines, Line{Raw: "", IsBlank: true})
		}
	}

	cfg.Lines = append(cfg.Lines, hostLine, rfLine)
	cfg.Blocks = buildBlocks(cfg.Lines)

	return nil
}

// RemoveRemoteForward removes the pastelocal RemoteForward line from the host block.
// Removes the entire Host block if it only contained the RemoteForward line.
func RemoveRemoteForward(cfg *Config, alias string, port int) error {
	blk := FindBlock(cfg, alias)
	if blk == nil {
		return nil
	}

	marker := fmt.Sprintf("# pastelocal:%s:remoteforward", alias)

	// Find the RemoteForward line with our marker for the given port.
	var foundIdx int = -1
	for i, l := range blk.Lines {
		if strings.ToLower(l.Keyword) == "remoteforward" &&
			isSamePort(l.Argument, port) &&
			strings.Contains(l.Raw, marker) {
			foundIdx = i
			break
		}
	}
	if foundIdx == -1 {
		return nil
	}

	// Count non-blank, non-comment lines in block (excluding the one we're removing).
	contentCount := 0
	for i, l := range blk.Lines {
		if i == foundIdx {
			continue
		}
		if !l.IsBlank && !l.IsComment {
			contentCount++
		}
	}

	// If removing this line leaves only the Host line (1 content line),
	// remove the entire block.
	if contentCount <= 1 {
		removeBlock(cfg, blk)
		return nil
	}

	// Just remove the line from both cfg.Lines and blk.Lines.
	// Find the line in cfg.Lines by its Raw content.
	rawToRemove := blk.Lines[foundIdx].Raw
	for i, l := range cfg.Lines {
		if l.Raw == rawToRemove && strings.ToLower(l.Keyword) == "remoteforward" {
			cfg.Lines = append(cfg.Lines[:i], cfg.Lines[i+1:]...)
			break
		}
	}
	blk.Lines = append(blk.Lines[:foundIdx], blk.Lines[foundIdx+1:]...)
	cfg.Blocks = buildBlocks(cfg.Lines)

	return nil
}

// HasRemoteForward checks if the host block has a RemoteForward for the port
// with the pastelocal marker.
func HasRemoteForward(cfg *Config, alias string, port int) bool {
	blk := FindBlock(cfg, alias)
	if blk == nil {
		return false
	}

	marker := fmt.Sprintf("# pastelocal:%s:remoteforward", alias)
	for _, l := range blk.Lines {
		if strings.ToLower(l.Keyword) == "remoteforward" &&
			isSamePort(l.Argument, port) &&
			strings.Contains(l.Raw, marker) {
			return true
		}
	}
	return false
}

// blockIndent returns the indentation used by the non-header lines in a block.
// Falls back to "  " if no indented lines are found.
func blockIndent(blk *Block) string {
	for _, l := range blk.Lines {
		if isBlockHeader(l) {
			continue
		}
		if l.Indent != "" {
			return l.Indent
		}
	}
	return "  "
}

// findInsertIndex returns the index in cfg.Lines where a new line should be
// inserted for the given block: after the last content line of the block.
func findInsertIndex(cfg *Config, blk *Block) int {
	startIdx := blockStartLine(cfg, blk)
	if startIdx == -1 {
		return len(cfg.Lines)
	}

	// Find the next block header after the start of this block.
	nextBlockIdx := len(cfg.Lines)
	for i := startIdx + 1; i < len(cfg.Lines); i++ {
		if isBlockHeader(cfg.Lines[i]) {
			nextBlockIdx = i
			break
		}
	}

	// Walk backwards from the next block header (or end) to find the last
	// non-blank, non-comment line that belongs to this block.
	insertAt := nextBlockIdx
	for i := nextBlockIdx - 1; i > startIdx; i-- {
		if !cfg.Lines[i].IsBlank && !cfg.Lines[i].IsComment {
			insertAt = i + 1
			break
		}
	}
	// If all lines between start and next block are blank/comments,
	// insert right after the header.
	if insertAt == nextBlockIdx && nextBlockIdx > startIdx+1 {
		insertAt = startIdx + 1
	}
	if insertAt <= startIdx {
		insertAt = startIdx + 1
	}

	return insertAt
}

// removeBlock removes all lines belonging to the given block from cfg.Lines,
// as well as any trailing blank line that separates it from the next block.
func removeBlock(cfg *Config, blk *Block) {
	startIdx := blockStartLine(cfg, blk)
	if startIdx == -1 {
		return
	}

	// Find the end: the last line before the next Host/Match or end-of-file.
	endIdx := len(cfg.Lines) - 1
	for i := startIdx + 1; i < len(cfg.Lines); i++ {
		if isBlockHeader(cfg.Lines[i]) {
			endIdx = i - 1
			break
		}
	}

	// Trim blank lines at the end of the block.
	for endIdx > startIdx && cfg.Lines[endIdx].IsBlank {
		endIdx--
	}

	// Also remove a preceding blank line (separator) if it exists.
	removeStart := startIdx
	if startIdx > 0 && cfg.Lines[startIdx-1].IsBlank {
		removeStart = startIdx - 1
	}

	// Remove lines from removeStart to endIdx inclusive.
	cfg.Lines = append(cfg.Lines[:removeStart], cfg.Lines[endIdx+1:]...)
	cfg.Blocks = buildBlocks(cfg.Lines)
}

// isSamePort checks whether a RemoteForward argument string uses the given
// local port. RemoteForward format is "listen_port connect_host:connect_port"
// or "listen_addr:listen_port connect_host:connect_port".
func isSamePort(argument string, port int) bool {
	// The first token before whitespace is the listen specification.
	parts := strings.Fields(argument)
	if len(parts) == 0 {
		return false
	}
	listenSpec := parts[0]

	// Could be just a port number, or host:port.
	if strings.Contains(listenSpec, ":") {
		// Take the part after the last colon.
		lastColon := strings.LastIndex(listenSpec, ":")
		portStr := listenSpec[lastColon+1:]
		return portStr == fmt.Sprintf("%d", port)
	}
	return listenSpec == fmt.Sprintf("%d", port)
}
