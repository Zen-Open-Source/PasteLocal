package sshconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomicCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")

	content := "Host myserver\n  HostName example.com\n"
	if err := WriteAtomic(path, content); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != content {
		t.Errorf("content mismatch:\ngot:  %q\nwant: %q", string(data), content)
	}
}

func TestWriteAtomicFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")

	if err := WriteAtomic(path, "test"); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("file permissions = %04o, want 0600", perm)
	}
}

func TestWriteAtomicOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")

	// Write initial content.
	if err := WriteAtomic(path, "initial content"); err != nil {
		t.Fatalf("first WriteAtomic: %v", err)
	}

	// Overwrite with new content.
	newContent := "new content"
	if err := WriteAtomic(path, newContent); err != nil {
		t.Fatalf("second WriteAtomic: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != newContent {
		t.Errorf("content mismatch:\ngot:  %q\nwant: %q", string(data), newContent)
	}
}

func TestWriteAtomicCreatesParentDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "nested", "config")

	if err := WriteAtomic(path, "test content"); err != nil {
		t.Fatalf("WriteAtomic with nested dirs: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "test content" {
		t.Errorf("content mismatch: got %q, want %q", string(data), "test content")
	}
}

func TestWriteAtomicEmptyContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")

	if err := WriteAtomic(path, ""); err != nil {
		t.Fatalf("WriteAtomic empty: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "" {
		t.Errorf("expected empty file, got %q", string(data))
	}
}

func TestWriteAtomicNoTempFileLeft(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")

	if err := WriteAtomic(path, "test"); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}

	// Verify no temp files remain in the directory.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if matched, _ := filepath.Match(".clipbridge-*.tmp", e.Name()); matched {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}
