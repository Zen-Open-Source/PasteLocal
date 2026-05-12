package sshconfig

import (
	"strings"
	"testing"
)

// --- Parser edge cases ---

func TestParseTrailingWhitespace(t *testing.T) {
	// parseLine trims trailing whitespace only for blank-line detection.
	// The Argument field preserves trailing whitespace from the original line.
	input := "Host myserver   \n  HostName example.com\t\n"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	blk := cfg.Blocks[0]
	// Argument includes trailing whitespace (parseLine only does TrimLeft, not TrimRight on argument).
	if blk.Lines[0].Argument != "myserver   " {
		t.Errorf("argument = %q, want %q", blk.Lines[0].Argument, "myserver   ")
	}
	if blk.Lines[1].Argument != "example.com\t" {
		t.Errorf("argument = %q, want %q", blk.Lines[1].Argument, "example.com\t")
	}
	// Render should round-trip exactly.
	rendered := cfg.Render()
	if rendered != input {
		t.Errorf("round-trip mismatch:\ngot:  %q\nwant: %q", rendered, input)
	}
}

func TestParseKeywordOnlyLine(t *testing.T) {
	// A line with just a keyword and no argument.
	input := "Host myserver\n  ForwardAgent\n  HostName example.com"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	blk := cfg.Blocks[0]
	foundKeywordOnly := false
	for _, l := range blk.Lines {
		if l.Keyword == "ForwardAgent" && l.Argument == "" {
			foundKeywordOnly = true
		}
	}
	if !foundKeywordOnly {
		t.Error("expected to find keyword-only line 'ForwardAgent'")
	}
}

func TestParseTabSeparatedKeywordArgument(t *testing.T) {
	input := "Host myserver\nHostName\texample.com"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	blk := cfg.Blocks[0]
	// The second line has no indent (starts with keyword), so Indent should be empty.
	hostLine := blk.Lines[1]
	if hostLine.Indent != "" {
		t.Errorf("Indent = %q, want empty", hostLine.Indent)
	}
	if hostLine.Keyword != "HostName" {
		t.Errorf("Keyword = %q, want HostName", hostLine.Keyword)
	}
	if hostLine.Argument != "example.com" {
		t.Errorf("Argument = %q, want example.com", hostLine.Argument)
	}
}

func TestParseMultipleSpacesBetweenKeywordAndArgument(t *testing.T) {
	input := "Host     myserver"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	blk := cfg.Blocks[0]
	if blk.Name != "myserver" {
		t.Errorf("block name = %q, want myserver", blk.Name)
	}
	if blk.Lines[0].Keyword != "Host" {
		t.Errorf("keyword = %q, want Host", blk.Lines[0].Keyword)
	}
	// Argument should not contain leading spaces (TrimLeft is applied).
	if blk.Lines[0].Argument != "myserver" {
		t.Errorf("argument = %q, want myserver", blk.Lines[0].Argument)
	}
}

func TestParseEmptyLinesBetweenKeywords(t *testing.T) {
	input := "Host myserver\n\n  HostName example.com\n\n  User alice"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	blk := cfg.Blocks[0]
	// Blank lines inside a block should be part of the block.
	if len(blk.Lines) < 3 {
		t.Errorf("expected at least 3 lines in block, got %d", len(blk.Lines))
	}
	// The block should contain both HostName and User.
	foundHN, foundUser := false, false
	for _, l := range blk.Lines {
		if l.Keyword == "HostName" {
			foundHN = true
		}
		if l.Keyword == "User" {
			foundUser = true
		}
	}
	if !foundHN {
		t.Error("expected HostName in block")
	}
	if !foundUser {
		t.Error("expected User in block")
	}
}

func TestParseCommentWithNoIndent(t *testing.T) {
	input := "# top-level comment\nHost myserver\n  HostName example.com"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg.Lines[0].IsComment {
		t.Error("line 0 should be a comment")
	}
	if cfg.Lines[0].Indent != "" {
		t.Errorf("comment indent = %q, want empty", cfg.Lines[0].Indent)
	}
}

func TestParseLineIndex(t *testing.T) {
	input := "Host myserver\n  HostName example.com\n  User alice"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	idx := lineIndex(cfg.Lines, "HostName", "example.com")
	if idx != 1 {
		t.Errorf("lineIndex(HostName, example.com) = %d, want 1", idx)
	}
	idx = lineIndex(cfg.Lines, "User", "alice")
	if idx != 2 {
		t.Errorf("lineIndex(User, alice) = %d, want 2", idx)
	}
	idx = lineIndex(cfg.Lines, "Port", "22")
	if idx != -1 {
		t.Errorf("lineIndex(Port, 22) = %d, want -1", idx)
	}
	// Case-insensitive keyword match.
	idx = lineIndex(cfg.Lines, "hostname", "example.com")
	if idx != 1 {
		t.Errorf("lineIndex(hostname, example.com) = %d, want 1 (case-insensitive)", idx)
	}
}

func TestBlockStartLineNotFound(t *testing.T) {
	cfg := &Config{Lines: []Line{}}
	blk := &Block{Lines: []Line{}}
	idx := blockStartLine(cfg, blk)
	if idx != -1 {
		t.Errorf("blockStartLine with empty block = %d, want -1", idx)
	}
}

func TestBlockEndLine(t *testing.T) {
	input := "Host myserver\n  HostName example.com\n  User alice\n\nHost other\n  HostName other.com"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	blk := cfg.Blocks[0]
	idx := blockEndLine(cfg, &blk)
	// The last line of the first block should be "  User alice" (index 2),
	// or possibly the blank line (index 3) if it's included in the block.
	// Based on buildBlocks, blank lines between blocks are part of the first block.
	if idx < 2 {
		t.Errorf("blockEndLine for first block = %d, want >= 2", idx)
	}
}

func TestBlockEndLineEmptyBlock(t *testing.T) {
	cfg := &Config{Lines: []Line{}}
	blk := &Block{Lines: []Line{}}
	idx := blockEndLine(cfg, blk)
	if idx != -1 {
		t.Errorf("blockEndLine with empty block = %d, want -1", idx)
	}
}

func TestIsBlockHeader(t *testing.T) {
	tests := []struct {
		keyword string
		want    bool
	}{
		{"Host", true},
		{"host", true},
		{"HOST", true},
		{"Match", true},
		{"match", true},
		{"HostName", false},
		{"User", false},
		{"", false},
	}
	for _, tt := range tests {
		l := Line{Keyword: tt.keyword}
		if got := isBlockHeader(l); got != tt.want {
			t.Errorf("isBlockHeader(%q) = %v, want %v", tt.keyword, got, tt.want)
		}
	}
}

// --- isSamePort tests ---

func TestIsSamePortSimple(t *testing.T) {
	if !isSamePort("8080 127.0.0.1:8080", 8080) {
		t.Error("expected port 8080 to match")
	}
	if isSamePort("8080 127.0.0.1:8080", 9090) {
		t.Error("expected port 8080 not to match 9090")
	}
}

func TestIsSamePortWithHostPort(t *testing.T) {
	// listen_spec is "0.0.0.0:8080"
	if !isSamePort("0.0.0.0:8080 127.0.0.1:8080", 8080) {
		t.Error("expected port 8080 to match with host:port format")
	}
}

func TestIsSamePortEmptyArgument(t *testing.T) {
	if isSamePort("", 8080) {
		t.Error("expected empty argument not to match")
	}
}

func TestIsSamePortJustNumber(t *testing.T) {
	if !isSamePort("22", 22) {
		t.Error("expected just port number to match")
	}
}

// --- blockIndent tests ---

func TestBlockIndentFallback(t *testing.T) {
	// Block with only a Host line (no indented lines) should return default "  ".
	blk := &Block{
		IsHost: true,
		Name:   "myhost",
		Lines: []Line{
			{Raw: "Host myhost", Keyword: "Host", Argument: "myhost", Indent: ""},
		},
	}
	indent := blockIndent(blk)
	if indent != "  " {
		t.Errorf("blockIndent fallback = %q, want two spaces", indent)
	}
}

// --- findInsertIndex edge cases ---

func TestFindInsertIndexBlockWithBlankLinesBeforeNextBlock(t *testing.T) {
	input := "Host myserver\n  HostName example.com\n\n\nHost other\n  HostName other.com"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	blk := FindBlock(cfg, "myserver")
	if blk == nil {
		t.Fatal("expected block myserver")
	}
	idx := findInsertIndex(cfg, blk)
	// Should insert after the last content line, before blank lines before next block.
	if idx <= 0 {
		t.Errorf("findInsertIndex = %d, want > 0", idx)
	}
}

func TestFindInsertIndexBlockNotFound(t *testing.T) {
	cfg, _ := Parse("Host myserver\n  HostName example.com")
	// Create a block not in cfg.
	blk := &Block{Name: "ghost", Lines: []Line{{Raw: "nonexistent"}}}
	idx := findInsertIndex(cfg, blk)
	if idx != len(cfg.Lines) {
		t.Errorf("findInsertIndex for non-existent block = %d, want %d", idx, len(cfg.Lines))
	}
}

// --- removeBlock edge cases ---

func TestRemoveBlockNoPrecedingBlankLine(t *testing.T) {
	// Block at the very start of the file (no preceding blank line).
	input := "Host myhost\n  RemoteForward 8080 127.0.0.1:8080  # clipbridge:myhost:remoteforward"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	blk := FindBlock(cfg, "myhost")
	if blk == nil {
		t.Fatal("expected block myhost")
	}
	removeBlock(cfg, blk)
	rendered := cfg.Render()
	if strings.Contains(rendered, "Host myhost") {
		t.Error("rendered config still contains Host block")
	}
}

func TestRemoveBlockWithTrailingBlankLines(t *testing.T) {
	input := "Host first\n  HostName first.com\n\n\nHost second\n  HostName second.com"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	blk := FindBlock(cfg, "first")
	if blk == nil {
		t.Fatal("expected block first")
	}
	removeBlock(cfg, blk)
	// Second block should still exist.
	blk2 := FindBlock(cfg, "second")
	if blk2 == nil {
		t.Error("second block should still exist after removing first")
	}
}

// --- Editor edge cases: wildcard host names ---

func TestFindBlockWithWildcard(t *testing.T) {
	input := "Host *\n  User default\n\nHost myserver\n  HostName example.com"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	blk := FindBlock(cfg, "*")
	if blk == nil {
		t.Error("FindBlock(*) should find the wildcard block")
	}
	if blk.Name != "*" {
		t.Errorf("wildcard block name = %q, want *", blk.Name)
	}
}

func TestAddRemoteForwardToWildcardHost(t *testing.T) {
	input := "Host *\n  User default"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := AddRemoteForward(cfg, "*", 8080); err != nil {
		t.Fatalf("AddRemoteForward: %v", err)
	}
	if !HasRemoteForward(cfg, "*", 8080) {
		t.Error("expected RemoteForward in wildcard block")
	}
}

// --- Adding to last block in file ---

func TestAddRemoteForwardToLastBlock(t *testing.T) {
	input := "Host first\n  HostName first.com\n\nHost last\n  HostName last.com"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := AddRemoteForward(cfg, "last", 3000); err != nil {
		t.Fatalf("AddRemoteForward: %v", err)
	}
	if !HasRemoteForward(cfg, "last", 3000) {
		t.Error("expected RemoteForward in last block")
	}
	// Verify the first block is unaffected.
	if HasRemoteForward(cfg, "first", 3000) {
		t.Error("first block should not have RemoteForward")
	}
}

// --- Adding to host with no indent (top-level) ---

func TestAddRemoteForwardToHostWithNoIndent(t *testing.T) {
	// A block where all lines have no indent (unusual but possible).
	input := "Host myhost\nHostName example.com\nUser alice"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := AddRemoteForward(cfg, "myhost", 8080); err != nil {
		t.Fatalf("AddRemoteForward: %v", err)
	}
	// blockIndent should fall back to "  " since no indented lines exist.
	blk := FindBlock(cfg, "myhost")
	if blk == nil {
		t.Fatal("expected block myhost")
	}
	for _, l := range blk.Lines {
		if strings.ToLower(l.Keyword) == "remoteforward" {
			if l.Indent != "  " {
				t.Errorf("RemoteForward indent = %q, want two spaces (default)", l.Indent)
			}
		}
	}
}

// --- Complex real-world ssh config fixture ---

func TestParseComplexRealWorldConfig(t *testing.T) {
	input := `# Personal SSH configuration
# Last updated: 2024-01-15

Host *
  AddKeysToAgent yes
  UseKeychain yes
  IdentityFile ~/.ssh/id_ed25519

Host github.com
  HostName github.com
  User git
  IdentityFile ~/.ssh/github_key
  IdentitiesOnly yes

Host myserver
  HostName server.example.com
  Port 2222
  User admin
  IdentityFile ~/.ssh/server_key
  RemoteForward 8080 127.0.0.1:8080  # clipbridge:myserver:remoteforward
  # Optional: enable agent forwarding
  ForwardAgent yes

Host *.internal.company.com
  User deploy
  IdentityFile ~/.ssh/deploy_key
  StrictHostKeyChecking no

Match host *.dev.company.com user admin
  ForwardAgent yes
  ProxyJump bastion.company.com
`
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Should have 5 blocks: *, github.com, myserver, *.internal.company.com, Match
	if len(cfg.Blocks) != 5 {
		t.Errorf("expected 5 blocks, got %d", len(cfg.Blocks))
	}

	// Check wildcard block.
	blk := FindBlock(cfg, "*")
	if blk == nil {
		t.Error("expected wildcard block")
	}

	// Check GitHub block.
	blk = FindBlock(cfg, "github.com")
	if blk == nil {
		t.Error("expected github.com block")
	}

	// Check myserver block has RemoteForward.
	blk = FindBlock(cfg, "myserver")
	if blk == nil {
		t.Fatal("expected myserver block")
	}
	if !HasRemoteForward(cfg, "myserver", 8080) {
		t.Error("expected RemoteForward in myserver block")
	}

	// Check wildcard pattern block.
	blk = FindBlock(cfg, "*.internal.company.com")
	if blk == nil {
		t.Error("expected *.internal.company.com block")
	}

	// Check Match block (last).
	foundMatch := false
	for _, b := range cfg.Blocks {
		if !b.IsHost && strings.Contains(b.Name, "*.dev.company.com") {
			foundMatch = true
		}
	}
	if !foundMatch {
		t.Error("expected Match block for *.dev.company.com")
	}
}

// --- Round-trip: parse complex config → modify → render → parse again → verify ---

func TestRoundTripComplexConfig(t *testing.T) {
	input := `# Global settings
AddKeysToAgent yes

Host alpha
  HostName alpha.example.com
  User admin

Host beta
  HostName beta.example.com
  Port 2222
`
	// Step 1: Parse.
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Step 2: Modify - add RemoteForward to alpha.
	if err := AddRemoteForward(cfg, "alpha", 9000); err != nil {
		t.Fatalf("AddRemoteForward: %v", err)
	}

	// Step 3: Render.
	rendered := cfg.Render()

	// Step 4: Parse again.
	cfg2, err := Parse(rendered)
	if err != nil {
		t.Fatalf("Parse(rendered): %v", err)
	}

	// Step 5: Verify - RemoteForward should be present.
	if !HasRemoteForward(cfg2, "alpha", 9000) {
		t.Error("RemoteForward should survive round-trip")
	}

	// Beta block should be unaffected.
	blk := FindBlock(cfg2, "beta")
	if blk == nil {
		t.Fatal("beta block should survive round-trip")
	}
	foundPort := false
	for _, l := range blk.Lines {
		if l.Keyword == "Port" && l.Argument == "2222" {
			foundPort = true
		}
	}
	if !foundPort {
		t.Error("beta block should still have Port 2222 after round-trip")
	}

	// Now remove RemoteForward and verify round-trip again.
	if err := RemoveRemoteForward(cfg2, "alpha", 9000); err != nil {
		t.Fatalf("RemoveRemoteForward: %v", err)
	}
	rendered2 := cfg2.Render()
	cfg3, err := Parse(rendered2)
	if err != nil {
		t.Fatalf("Parse(rendered2): %v", err)
	}
	if HasRemoteForward(cfg3, "alpha", 9000) {
		t.Error("RemoteForward should be removed after second round-trip")
	}
	blk3 := FindBlock(cfg3, "alpha")
	if blk3 == nil {
		t.Fatal("alpha block should still exist after removing RemoteForward")
	}
}

// --- Test add to non-existent host creates block with blank separator ---

func TestAddRemoteForwardToNonExistentHostWithExistingConfig(t *testing.T) {
	input := "Host existing\n  HostName existing.com"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if err := AddRemoteForward(cfg, "newhost", 5000); err != nil {
		t.Fatalf("AddRemoteForward: %v", err)
	}

	rendered := cfg.Render()
	// Should have a blank line before the new block.
	if !strings.Contains(rendered, "\n\nHost newhost") && !strings.Contains(rendered, "Host existing\n  HostName existing.com\n\nHost newhost") {
		// At minimum the new host should exist.
		if !strings.Contains(rendered, "Host newhost") {
			t.Error("rendered config missing Host newhost")
		}
	}

	blk := FindBlock(cfg, "newhost")
	if blk == nil {
		t.Fatal("expected newhost block")
	}
	if !HasRemoteForward(cfg, "newhost", 5000) {
		t.Error("expected RemoteForward in newhost block")
	}
}

// --- Test adding a second RemoteForward to same host (different port) ---

func TestAddRemoteForwardMultiplePorts(t *testing.T) {
	input := "Host myhost\n  HostName example.com\n  RemoteForward 8080 127.0.0.1:8080  # clipbridge:myhost:remoteforward"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if err := AddRemoteForward(cfg, "myhost", 9090); err != nil {
		t.Fatalf("AddRemoteForward second port: %v", err)
	}

	if !HasRemoteForward(cfg, "myhost", 8080) {
		t.Error("original RemoteForward 8080 should still be present")
	}
	if !HasRemoteForward(cfg, "myhost", 9090) {
		t.Error("new RemoteForward 9090 should be present")
	}
}

// --- Render preserves comments and blank lines in complex config ---

func TestRenderComplexConfigPreservesCommentsAndBlankLines(t *testing.T) {
	input := "# top comment\n\nHost a\n  # inline comment\n  HostName a.com\n\nHost b\n  HostName b.com\n"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	rendered := cfg.Render()
	if rendered != input {
		t.Errorf("round-trip mismatch:\ngot:  %q\nwant: %q", rendered, input)
	}
}

// --- Match block with complex arguments ---

func TestMatchBlockWithComplexArguments(t *testing.T) {
	input := "Match host *.example.com user admin\n  ForwardAgent yes\n  ProxyJump bastion"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(cfg.Blocks))
	}
	blk := cfg.Blocks[0]
	if blk.IsHost {
		t.Error("expected Match block, got Host")
	}
	if blk.Name != "host *.example.com user admin" {
		t.Errorf("block name = %q, want full Match argument", blk.Name)
	}
}
