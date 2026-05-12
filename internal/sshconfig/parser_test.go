package sshconfig

import (
	"testing"
)

func TestParseEmpty(t *testing.T) {
	cfg, err := Parse("")
	if err != nil {
		t.Fatalf("Parse empty: %v", err)
	}
	if len(cfg.Lines) != 0 {
		t.Errorf("expected 0 lines, got %d", len(cfg.Lines))
	}
	if len(cfg.Blocks) != 0 {
		t.Errorf("expected 0 blocks, got %d", len(cfg.Blocks))
	}
}

func TestParseSingleHostBlock(t *testing.T) {
	input := `Host myserver
  HostName example.com
  User alice`
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(cfg.Blocks))
	}
	blk := cfg.Blocks[0]
	if blk.Name != "myserver" {
		t.Errorf("block name = %q, want %q", blk.Name, "myserver")
	}
	if !blk.IsHost {
		t.Error("block should be a Host block")
	}
	if len(blk.Lines) != 3 {
		t.Errorf("expected 3 lines in block, got %d", len(blk.Lines))
	}
	if blk.Lines[0].Keyword != "Host" {
		t.Errorf("first line keyword = %q, want Host", blk.Lines[0].Keyword)
	}
	if blk.Lines[1].Keyword != "HostName" {
		t.Errorf("second line keyword = %q, want HostName", blk.Lines[1].Keyword)
	}
	if blk.Lines[1].Argument != "example.com" {
		t.Errorf("second line argument = %q, want example.com", blk.Lines[1].Argument)
	}
	if blk.Lines[1].Indent != "  " {
		t.Errorf("second line indent = %q, want two spaces", blk.Lines[1].Indent)
	}
}

func TestParseMultipleHostBlocks(t *testing.T) {
	input := `Host server1
  HostName one.example.com

Host server2
  HostName two.example.com`
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(cfg.Blocks))
	}
	if cfg.Blocks[0].Name != "server1" {
		t.Errorf("block[0] name = %q, want server1", cfg.Blocks[0].Name)
	}
	if cfg.Blocks[1].Name != "server2" {
		t.Errorf("block[1] name = %q, want server2", cfg.Blocks[1].Name)
	}
}

func TestParseWithCommentsAndBlankLines(t *testing.T) {
	input := `# This is a comment

Host myserver
  # Server details
  HostName example.com

  User alice
`
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// First line should be a comment.
	if !cfg.Lines[0].IsComment {
		t.Error("line 0 should be a comment")
	}
	// Second line should be blank.
	if !cfg.Lines[1].IsBlank {
		t.Error("line 1 should be blank")
	}

	// Check that the comment inside the block is preserved.
	blk := cfg.Blocks[0]
	foundComment := false
	for _, l := range blk.Lines {
		if l.IsComment && l.Raw == "  # Server details" {
			foundComment = true
			break
		}
	}
	if !foundComment {
		t.Error("expected to find comment '  # Server details' in block lines")
	}
}

func TestParseWithIndentedKeywords(t *testing.T) {
	input := "Host myserver\n\tHostName example.com\n\tUser alice"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	blk := cfg.Blocks[0]
	if blk.Lines[1].Indent != "\t" {
		t.Errorf("indent = %q, want tab", blk.Lines[1].Indent)
	}
	if blk.Lines[1].Keyword != "HostName" {
		t.Errorf("keyword = %q, want HostName", blk.Lines[1].Keyword)
	}
}

func TestParseWithGlobalLines(t *testing.T) {
	input := `AddKeysToAgent yes
Host myserver
  HostName example.com`
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Global line should be in cfg.Lines but not part of any block.
	if cfg.Lines[0].Keyword != "AddKeysToAgent" {
		t.Errorf("line[0] keyword = %q, want AddKeysToAgent", cfg.Lines[0].Keyword)
	}
	// Only one block (the Host block).
	if len(cfg.Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(cfg.Blocks))
	}
	if cfg.Blocks[0].Name != "myserver" {
		t.Errorf("block name = %q, want myserver", cfg.Blocks[0].Name)
	}
}

func TestRenderPreservesFormatting(t *testing.T) {
	input := "Host myserver\n  HostName example.com\n  User alice"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	rendered := cfg.Render()
	if rendered != input {
		t.Errorf("Render mismatch:\ngot:  %q\nwant: %q", rendered, input)
	}
}

func TestRoundTrip(t *testing.T) {
	tests := []string{
		"",
		"Host a\n  HostName b\n",
		"# comment\nHost a\n  HostName b\n\nHost c\n  User d\n",
		"GlobalKeyword value\n\nHost a\n  HostName b\n",
		"Host a\n\tHostName b\n\tUser c\n",
	}
	for _, input := range tests {
		cfg, err := Parse(input)
		if err != nil {
			t.Fatalf("Parse(%q): %v", input, err)
		}
		rendered := cfg.Render()
		if rendered != input {
			t.Errorf("round-trip mismatch:\ninput:    %q\nrendered: %q", input, rendered)
		}
	}
}

func TestRenderEmpty(t *testing.T) {
	cfg := &Config{}
	if got := cfg.Render(); got != "" {
		t.Errorf("Render empty config = %q, want empty string", got)
	}
}

func TestParseMatchBlock(t *testing.T) {
	input := `Match host *.example.com
  User deploy
  IdentityFile ~/.ssh/deploy_key`
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
	if blk.Name != "host *.example.com" {
		t.Errorf("block name = %q, want 'host *.example.com'", blk.Name)
	}
}

func TestParseCaseInsensitiveKeywords(t *testing.T) {
	input := "host myserver\n  hostname example.com\n  user alice"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	blk := cfg.Blocks[0]
	// Original keyword casing preserved in Keyword field.
	if blk.Lines[0].Keyword != "host" {
		t.Errorf("keyword = %q, want 'host'", blk.Lines[0].Keyword)
	}
	if blk.Lines[1].Keyword != "hostname" {
		t.Errorf("keyword = %q, want 'hostname'", blk.Lines[1].Keyword)
	}
}
