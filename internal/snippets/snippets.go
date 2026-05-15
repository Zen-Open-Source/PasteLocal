package snippets

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Snippet represents a saved clipboard snippet.
type Snippet struct {
	Name        string    `json:"name"`
	Format      string    `json:"format"` // "text" or "png"
	Data        []byte    `json:"data"`   // raw bytes (base64 for images)
	Size        int64     `json:"size"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Description string    `json:"description,omitempty"`
	Hash        string    `json:"hash"` // SHA-256 for integrity
}

// Store manages snippet storage on disk.
type Store struct {
	dir   string
	mu    sync.RWMutex
	maxSize int64 // max total size in bytes
}

// NewStore creates a new snippet store at the given directory.
func NewStore(dir string, maxSizeMB int64) *Store {
	if maxSizeMB <= 0 {
		maxSizeMB = 50 // default 50MB
	}
	return &Store{
		dir:     dir,
		maxSize: maxSizeMB * 1024 * 1024,
	}
}

// DefaultStorePath returns the default snippets directory.
func DefaultStorePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "pastelocal", "snippets")
}

// Save persists a snippet to disk.
func (s *Store) Save(name string, format string, data []byte, description string) error {
	if !isValidName(name) {
		return fmt.Errorf("invalid snippet name: %s (must be alphanumeric with - and _)", name)
	}

	if format != "text" && format != "png" {
		return fmt.Errorf("unsupported format: %s (must be 'text' or 'png')", format)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure directory exists.
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return fmt.Errorf("creating snippets directory: %w", err)
	}

	// Check total size limit.
	currentSize := s.totalSize()
	if currentSize+int64(len(data)) > s.maxSize {
		return fmt.Errorf("snippet storage full (max %d MB)", s.maxSize/(1024*1024))
	}

	hash := sha256.Sum256(data)
	snippet := Snippet{
		Name:        name,
		Format:      format,
		Data:        data,
		Size:        int64(len(data)),
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Description: description,
		Hash:        fmt.Sprintf("%x", hash),
	}

	// Check if updating existing.
	existingPath := filepath.Join(s.dir, name+".json")
	if _, err := os.Stat(existingPath); err == nil {
		// Load existing to preserve CreatedAt.
		if existing, err := s.loadUnlocked(name); err == nil {
			snippet.CreatedAt = existing.CreatedAt
		}
	}

	// Marshal and save atomically.
	jsonData, err := json.Marshal(snippet)
	if err != nil {
		return fmt.Errorf("marshaling snippet: %w", err)
	}

	path := filepath.Join(s.dir, name+".json")
	tmpPath := path + ".tmp"

	if err := os.WriteFile(tmpPath, jsonData, 0600); err != nil {
		return fmt.Errorf("writing snippet: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("finalizing snippet: %w", err)
	}

	return nil
}

// Load retrieves a snippet by name.
func (s *Store) Load(name string) (*Snippet, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.loadUnlocked(name)
}

// loadUnlocked loads a snippet without holding the lock.
func (s *Store) loadUnlocked(name string) (*Snippet, error) {
	path := filepath.Join(s.dir, name+".json")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("snippet not found: %s", name)
		}
		return nil, fmt.Errorf("reading snippet: %w", err)
	}

	var snippet Snippet
	if err := json.Unmarshal(data, &snippet); err != nil {
		return nil, fmt.Errorf("parsing snippet: %w", err)
	}

	// Verify integrity.
	hash := sha256.Sum256(snippet.Data)
	if fmt.Sprintf("%x", hash) != snippet.Hash {
		return nil, fmt.Errorf("snippet integrity check failed: %s", name)
	}

	return &snippet, nil
}

// Delete removes a snippet.
func (s *Store) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.dir, name+".json")
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("snippet not found: %s", name)
		}
		return fmt.Errorf("deleting snippet: %w", err)
	}

	return nil
}

// List returns all snippet names and metadata (without data).
func (s *Store) List() ([]Snippet, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Snippet{}, nil
		}
		return nil, fmt.Errorf("reading snippets directory: %w", err)
	}

	var snippets []Snippet
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".json")
		snippet, err := s.loadUnlocked(name)
		if err != nil {
			continue // skip corrupted
		}

		// Return without the actual data for list view.
		snippets = append(snippets, Snippet{
			Name:        snippet.Name,
			Format:      snippet.Format,
			Size:        snippet.Size,
			CreatedAt:   snippet.CreatedAt,
			UpdatedAt:   snippet.UpdatedAt,
			Description: snippet.Description,
			Hash:        snippet.Hash,
		})
	}

	return snippets, nil
}

// Exists checks if a snippet exists.
func (s *Store) Exists(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.dir, name+".json")
	_, err := os.Stat(path)
	return err == nil
}

// totalSize returns the total size of all snippets.
func (s *Store) totalSize() int64 {
	var total int64
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return 0
	}

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		total += info.Size()
	}

	return total
}

// isValidName checks if a snippet name is valid.
func isValidName(name string) bool {
	if name == "" {
		return false
	}
	if len(name) > 64 {
		return false
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
