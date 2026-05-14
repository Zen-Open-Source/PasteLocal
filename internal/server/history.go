package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/pastelocal/pastelocal/internal/proto"
)

// historyEntry wraps a clipboard content snapshot with metadata.
type historyEntry struct {
	ID         string
	Format     string
	Data       []byte // encrypted at rest
	ByteCount  int64
	Hash       string
	CapturedAt time.Time
}

// HistoryBuffer is an in-memory ring buffer that retains the last N
// clipboard reads. Data is encrypted at rest using AES-GCM with a
// key derived from the auth token.
type HistoryBuffer struct {
	mu     sync.Mutex
	entries []*historyEntry
	size   int
	ttl    time.Duration
	key    []byte // AES key derived from token
}

// NewHistoryBuffer creates a new HistoryBuffer with the given capacity and TTL.
func NewHistoryBuffer(size int, ttlSeconds int, token string) *HistoryBuffer {
	if size <= 0 {
		size = 1
	}
	h := &HistoryBuffer{
		entries: make([]*historyEntry, 0, size),
		size:    size,
		ttl:     time.Duration(ttlSeconds) * time.Second,
	}
	if token != "" {
		h.key = deriveKey(token)
	}
	return h
}

// Add adds a new entry to the buffer, evicting the oldest if full.
func (h *HistoryBuffer) Add(id, format string, data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()

	var encrypted []byte
	var err error
	if h.key != nil {
		encrypted, err = encrypt(h.key, data)
		if err != nil {
			// Fall back to storing unencrypted rather than losing data.
			encrypted = data
		}
	} else {
		encrypted = data
	}

	hash := sha256.Sum256(data)
	entry := &historyEntry{
		ID:         id,
		Format:     format,
		Data:       encrypted,
		ByteCount:  int64(len(data)),
		Hash:       fmt.Sprintf("%x", hash),
		CapturedAt: time.Now().UTC(),
	}

	if len(h.entries) >= h.size {
		h.entries = h.entries[1:]
	}
	h.entries = append(h.entries, entry)
}

// List returns the list of non-expired history entries as proto types.
func (h *HistoryBuffer) List() []proto.HistoryEntry {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now().UTC()
	var result []proto.HistoryEntry
	for _, e := range h.entries {
		if h.ttl > 0 && now.Sub(e.CapturedAt) > h.ttl {
			continue
		}
		result = append(result, proto.HistoryEntry{
			ID:         e.ID,
			CapturedAt: e.CapturedAt.Format(time.RFC3339),
			Format:     e.Format,
			ByteCount:  e.ByteCount,
			Hash:       e.Hash,
		})
	}
	return result
}

// Get returns the decrypted data for a specific history entry by ID.
// Returns nil if not found or expired.
func (h *HistoryBuffer) Get(id string) ([]byte, *historyEntry, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now().UTC()
	for _, e := range h.entries {
		if e.ID != id {
			continue
		}
		if h.ttl > 0 && now.Sub(e.CapturedAt) > h.ttl {
			return nil, nil, fmt.Errorf("entry expired")
		}

		var data []byte
		var err error
		if h.key != nil {
			data, err = decrypt(h.key, e.Data)
			if err != nil {
				return nil, nil, fmt.Errorf("decryption failed: %w", err)
			}
		} else {
			data = e.Data
		}
		return data, e, nil
	}
	return nil, nil, fmt.Errorf("entry not found")
}

// Len returns the number of entries in the buffer (including expired).
func (h *HistoryBuffer) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.entries)
}

// UpdateKey re-derives the encryption key from a new token.
func (h *HistoryBuffer) UpdateKey(token string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if token != "" {
		h.key = deriveKey(token)
	}
}

// deriveKey derives a 32-byte AES key from the token using SHA-256.
func deriveKey(token string) []byte {
	hash := sha256.Sum256([]byte("pastelocal-history-key:" + token))
	return hash[:]
}

// encrypt encrypts data using AES-GCM with the given key.
func encrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// decrypt decrypts data using AES-GCM with the given key.
func decrypt(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// encodeBase64 is a convenience wrapper.
func encodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}
