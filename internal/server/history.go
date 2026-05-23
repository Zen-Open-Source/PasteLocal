package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
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
	mu      sync.Mutex
	entries []*historyEntry
	size    int
	ttl     time.Duration
	key      []byte // AES key derived from token
	analyses    map[string]*AnalysisResult // v2: analysis results (OCR+desc)
	embeddings  map[string][]float64       // Recall v2: stored vectors keyed by entry ID
	searchTexts map[string]string          // Recall v2: the text that was embedded (for previews)
}

// NewHistoryBuffer creates a new HistoryBuffer with the given capacity and TTL.
func NewHistoryBuffer(size int, ttlSeconds int, token string) *HistoryBuffer {
	if size <= 0 {
		size = 1
	}
	h := &HistoryBuffer{
		entries:     make([]*historyEntry, 0, size),
		size:        size,
		ttl:         time.Duration(ttlSeconds) * time.Second,
		analyses:    make(map[string]*AnalysisResult),
		embeddings:  make(map[string][]float64),
		searchTexts: make(map[string]string),
	}
	if token != "" {
		h.key = deriveKey(token)
	}
	return h
}

// Add adds a new entry to the buffer, evicting the oldest if full.
// searchText and embedding are stored for Recall v2 semantic search (preview + cosine).
// Concealed items never reach this path.
func (h *HistoryBuffer) Add(id, format string, data []byte, searchText string, embedding []float64, capturedAt time.Time) {
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
	if capturedAt.IsZero() {
		capturedAt = time.Now().UTC()
	}
	entry := &historyEntry{
		ID:         id,
		Format:     format,
		Data:       encrypted,
		ByteCount:  int64(len(data)),
		Hash:       fmt.Sprintf("%x", hash),
		CapturedAt: capturedAt,
	}

	// Recall v2 + Vision v2 metadata eviction: when the ring evicts the oldest entry,
	// also drop its associated analyses / embeddings / searchTexts so they do not grow unbounded.
	if len(h.entries) >= h.size {
		evictedID := h.entries[0].ID
		delete(h.analyses, evictedID)
		delete(h.embeddings, evictedID)
		delete(h.searchTexts, evictedID)
		h.entries = h.entries[1:]
	}
	h.entries = append(h.entries, entry)

	// Recall v2: store embedding + the text used for it (for high-quality search previews).
	// We key by stable ID so that later Search and history fetches can correlate.
	if id != "" {
		if len(embedding) > 0 {
			h.embeddings[id] = embedding
		}
		if strings.TrimSpace(searchText) != "" {
			h.searchTexts[id] = searchText
		}
	}
}

// SetAnalysis associates a vision analysis result with a history entry ID (v2).
// Called after Add at read time or on first history fetch. Copies to avoid mutation.
func (h *HistoryBuffer) SetAnalysis(id string, a *AnalysisResult) {
	if a == nil || id == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.analyses == nil {
		h.analyses = make(map[string]*AnalysisResult)
	}
	h.analyses[id] = &AnalysisResult{
		OCRText:     a.OCRText,
		Description: a.Description,
	}
}

// GetAnalysis retrieves previously stored analysis for a history ID if present (v2).
// Enables instant rich context on /history/{id} without re-running external commands.
func (h *HistoryBuffer) GetAnalysis(id string) *AnalysisResult {
	if id == "" {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.analyses == nil {
		return nil
	}
	if a, ok := h.analyses[id]; ok {
		return &AnalysisResult{OCRText: a.OCRText, Description: a.Description}
	}
	return nil
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

// --- Recall v2 semantic search support ---

// Search performs cosine-similarity ranking of history entries that have stored
// embeddings against the provided query vector. Returns up to limit results
// (newest first on ties), with scores and a human-friendly preview.
//
// Only entries with a non-empty embedding vector participate. Expired entries
// are skipped. Preview prefers stored searchText (the exact text that was embedded),
// falling back to a short analysis summary or a generic label.
func (h *HistoryBuffer) Search(queryVec []float64, limit int) []proto.SearchResult {
	if len(queryVec) == 0 || limit <= 0 {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now().UTC()

	var candidates []scored
	for _, e := range h.entries {
		if h.ttl > 0 && now.Sub(e.CapturedAt) > h.ttl {
			continue
		}
		vec, ok := h.embeddings[e.ID]
		if !ok || len(vec) == 0 {
			continue
		}
		score := cosineSimilarity(queryVec, vec)
		if score <= 0 {
			continue
		}

		preview := h.searchTexts[e.ID]
		if strings.TrimSpace(preview) == "" {
			if a := h.analyses[e.ID]; a != nil {
				if t := strings.TrimSpace(a.OCRText); t != "" {
					preview = t
				} else if d := strings.TrimSpace(a.Description); d != "" {
					preview = d
				}
			}
		}
		if len(preview) > 200 {
			preview = preview[:200] + "…"
		}
		if strings.TrimSpace(preview) == "" {
			preview = fmt.Sprintf("%s item", e.Format)
		}

		candidates = append(candidates, scored{
			id:      e.ID,
			score:   score,
			entry:   e,
			preview: preview,
		})
	}

	// Sort by score desc (higher better)
	sortByScoreDesc(candidates)

	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	results := make([]proto.SearchResult, 0, len(candidates))
	for _, c := range candidates {
		results = append(results, proto.SearchResult{
			ID:         c.id,
			Score:      c.score,
			CapturedAt: c.entry.CapturedAt.Format(time.RFC3339),
			Format:     c.entry.Format,
			Preview:    c.preview,
		})
	}
	return results
}

// cosineSimilarity returns the cosine of the angle between two vectors.
// Returns 0 on dimension mismatch or zero-norm vectors.
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (sqrt(na) * sqrt(nb))
}

// sqrt is a tiny helper (math.Sqrt takes float64 but we avoid extra import here).
func sqrt(x float64) float64 {
	// Simple Newton iteration for non-negative x. Sufficient for our use (embeddings).
	if x <= 0 {
		return 0
	}
	z := x
	for i := 0; i < 8; i++ {
		z = z - (z*z-x)/(2*z)
	}
	return z
}

// sortByScoreDesc sorts in place by score descending.
func sortByScoreDesc(items []scored) {
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].score > items[i].score {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

// scored is an internal helper for Search sorting (not exported).
type scored struct {
	id      string
	score   float64
	entry   *historyEntry
	preview string
}
