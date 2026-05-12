package sshconfig

import (
	"strings"
	"testing"
)

func TestAddRemoteForwardToNonExistentHost(t *testing.T) {
	cfg, err := Parse("")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if err := AddRemoteForward(cfg, "myhost", 8080); err != nil {
		t.Fatalf("AddRemoteForward: %v", err)
	}

	blk := FindBlock(cfg, "myhost")
	if blk == nil {
		t.Fatal("expected block 'myhost' to be created")
	}
	if !blk.IsHost {
		t.Error("expected Host block")
	}
	if !HasRemoteForward(cfg, "myhost", 8080) {
		t.Error("expected RemoteForward to be present")
	}

	rendered := cfg.Render()
	if !strings.Contains(rendered, "Host myhost") {
		t.Error("rendered config missing 'Host myhost'")
	}
	if !strings.Contains(rendered, "RemoteForward 8080 127.0.0.1:8080") {
		t.Error("rendered config missing RemoteForward line")
	}
	marker := "# clipbridge:myhost:remoteforward"
	if !strings.Contains(rendered, marker) {
		t.Error("rendered config missing marker comment")
	}
}

func TestAddRemoteForwardToExistingHost(t *testing.T) {
	input := "Host myhost\n  HostName example.com\n  User alice"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if err := AddRemoteForward(cfg, "myhost", 8080); err != nil {
		t.Fatalf("AddRemoteForward: %v", err)
	}

	if !HasRemoteForward(cfg, "myhost", 8080) {
		t.Error("expected RemoteForward to be present")
	}

	rendered := cfg.Render()
	if !strings.Contains(rendered, "RemoteForward 8080 127.0.0.1:8080") {
		t.Error("rendered config missing RemoteForward line")
	}
}

func TestAddRemoteForwardAlreadyPresent(t *testing.T) {
	// Create config with an existing clipbridge RemoteForward.
	input := "Host myhost\n  HostName example.com\n  RemoteForward 8080 127.0.0.1:8080  # clipbridge:myhost:remoteforward"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	err = AddRemoteForward(cfg, "myhost", 8080)
	if err != nil {
		t.Fatalf("AddRemoteForward should be no-op, got error: %v", err)
	}

	// Should not have added a duplicate line.
	rfCount := 0
	for _, l := range cfg.Lines {
		if strings.ToLower(l.Keyword) == "remoteforward" {
			rfCount++
		}
	}
	if rfCount != 1 {
		t.Errorf("expected 1 RemoteForward line, got %d", rfCount)
	}
}

func TestAddRemoteForwardConflictingPort(t *testing.T) {
	input := "Host myhost\n  HostName example.com\n  RemoteForward 8080 127.0.0.1:9090"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	err = AddRemoteForward(cfg, "myhost", 8080)
	if err == nil {
		t.Fatal("expected error for conflicting RemoteForward, got nil")
	}
	if !strings.Contains(err.Error(), "conflicting") {
		t.Errorf("error message should mention 'conflicting', got: %v", err)
	}
}

func TestRemoveRemoteForwardRemovesLine(t *testing.T) {
	input := "Host myhost\n  HostName example.com\n  RemoteForward 8080 127.0.0.1:8080  # clipbridge:myhost:remoteforward\n  User alice"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if err := RemoveRemoteForward(cfg, "myhost", 8080); err != nil {
		t.Fatalf("RemoveRemoteForward: %v", err)
	}

	if HasRemoteForward(cfg, "myhost", 8080) {
		t.Error("RemoteForward should be removed")
	}

	// Host block should still exist.
	blk := FindBlock(cfg, "myhost")
	if blk == nil {
		t.Fatal("Host block should still exist")
	}

	rendered := cfg.Render()
	if strings.Contains(rendered, "RemoteForward 8080") {
		t.Error("rendered config still contains RemoteForward line")
	}
}

func TestRemoveRemoteForwardRemovesEmptyBlock(t *testing.T) {
	input := "Host myhost\n  RemoteForward 8080 127.0.0.1:8080  # clipbridge:myhost:remoteforward"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if err := RemoveRemoteForward(cfg, "myhost", 8080); err != nil {
		t.Fatalf("RemoveRemoteForward: %v", err)
	}

	blk := FindBlock(cfg, "myhost")
	if blk != nil {
		t.Error("Host block should be removed entirely")
	}

	rendered := cfg.Render()
	if strings.Contains(rendered, "Host myhost") {
		t.Error("rendered config still contains Host block")
	}
}

func TestHasRemoteForwardDetectsPresence(t *testing.T) {
	input := "Host myhost\n  RemoteForward 8080 127.0.0.1:8080  # clipbridge:myhost:remoteforward"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if !HasRemoteForward(cfg, "myhost", 8080) {
		t.Error("expected HasRemoteForward to return true")
	}
	if HasRemoteForward(cfg, "myhost", 9090) {
		t.Error("expected HasRemoteForward to return false for different port")
	}
	if HasRemoteForward(cfg, "otherhost", 8080) {
		t.Error("expected HasRemoteForward to return false for different host")
	}
}

func TestHasRemoteForwardNoMarker(t *testing.T) {
	// A RemoteForward without the clipbridge marker should not be detected.
	input := "Host myhost\n  RemoteForward 8080 127.0.0.1:8080"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if HasRemoteForward(cfg, "myhost", 8080) {
		t.Error("expected HasRemoteForward to return false without marker")
	}
}

func TestIndentationMatchesExistingBlock(t *testing.T) {
	input := "Host myhost\n    HostName example.com\n    User alice"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if err := AddRemoteForward(cfg, "myhost", 8080); err != nil {
		t.Fatalf("AddRemoteForward: %v", err)
	}

	blk := FindBlock(cfg, "myhost")
	if blk == nil {
		t.Fatal("expected block to exist")
	}

	// Find the RemoteForward line in the block.
	for _, l := range blk.Lines {
		if strings.ToLower(l.Keyword) == "remoteforward" {
			if l.Indent != "    " {
				t.Errorf("RemoteForward indent = %q, want 4 spaces", l.Indent)
			}
			if !strings.HasPrefix(l.Raw, "    ") {
				t.Errorf("RemoteForward raw line should start with 4 spaces, got: %q", l.Raw)
			}
			break
		}
	}
}

func TestCommentMarkerAdded(t *testing.T) {
	cfg, err := Parse("")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if err := AddRemoteForward(cfg, "myhost", 8080); err != nil {
		t.Fatalf("AddRemoteForward: %v", err)
	}

	rendered := cfg.Render()
	marker := "# clipbridge:myhost:remoteforward"
	if !strings.Contains(rendered, marker) {
		t.Errorf("rendered config missing marker %q, got: %q", marker, rendered)
	}

	// The marker should be on the same line as RemoteForward.
	for _, l := range cfg.Lines {
		if strings.ToLower(l.Keyword) == "remoteforward" {
			if !strings.Contains(l.Raw, marker) {
				t.Errorf("RemoteForward line should contain marker, got: %q", l.Raw)
			}
		}
	}
}

func TestFindBlock(t *testing.T) {
	input := "Host alpha\n  HostName a.com\n\nHost beta\n  HostName b.com"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if blk := FindBlock(cfg, "alpha"); blk == nil {
		t.Error("FindBlock(alpha) returned nil")
	}
	if blk := FindBlock(cfg, "beta"); blk == nil {
		t.Error("FindBlock(beta) returned nil")
	}
	if blk := FindBlock(cfg, "gamma"); blk != nil {
		t.Error("FindBlock(gamma) should return nil")
	}
}

func TestAddRemoteForwardToExistingHostWithMultipleBlocks(t *testing.T) {
	input := "Host first\n  HostName first.com\n\nHost second\n  HostName second.com"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if err := AddRemoteForward(cfg, "second", 9090); err != nil {
		t.Fatalf("AddRemoteForward: %v", err)
	}

	// Verify the RemoteForward is in the second block only.
	blkFirst := FindBlock(cfg, "first")
	if blkFirst != nil {
		for _, l := range blkFirst.Lines {
			if strings.ToLower(l.Keyword) == "remoteforward" {
				t.Error("first block should not have RemoteForward")
			}
		}
	}

	blkSecond := FindBlock(cfg, "second")
	if blkSecond == nil {
		t.Fatal("second block should exist")
	}
	if !HasRemoteForward(cfg, "second", 9090) {
		t.Error("second block should have RemoteForward")
	}
}

func TestRemoveRemoteForwardNonExistentHost(t *testing.T) {
	cfg, err := Parse("Host other\n  HostName other.com")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Should not error.
	if err := RemoveRemoteForward(cfg, "nonexistent", 8080); err != nil {
		t.Fatalf("RemoveRemoteForward on nonexistent host: %v", err)
	}
}

func TestRemoveRemoteForwardNonExistentPort(t *testing.T) {
	input := "Host myhost\n  RemoteForward 8080 127.0.0.1:8080  # clipbridge:myhost:remoteforward"
	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Removing a port that doesn't exist should be a no-op.
	if err := RemoveRemoteForward(cfg, "myhost", 9999); err != nil {
		t.Fatalf("RemoveRemoteForward for non-existent port: %v", err)
	}

	// Original RemoteForward should still be there.
	if !HasRemoteForward(cfg, "myhost", 8080) {
		t.Error("original RemoteForward should still be present")
	}
}
