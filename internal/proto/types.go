package proto

// ProtocolVersion is the current wire protocol version.
const ProtocolVersion = 4


// ClipboardResponse is the JSON response for GET /clipboard on success.
type ClipboardResponse struct {
	OK         bool   `json:"ok"`
	Image      string `json:"image,omitempty"` // base64-encoded image (png/jpeg/etc)
	Text       string `json:"text,omitempty"`  // plain text content (when format is text)
	Format     string `json:"format"`          // "png", "text", "html"
	ByteCount  int64  `json:"byte_count"`
	CapturedAt string `json:"captured_at"`  // RFC3339
	ID         string `json:"id,omitempty"` // unique ID for history entries
	Analysis   *ClipboardAnalysis `json:"analysis,omitempty"`
}

// ClipboardWriteRequest is the JSON request for POST /clipboard.
type ClipboardWriteRequest struct {
	Format string `json:"format"`          // "png" or "text"
	Image  string `json:"image,omitempty"` // base64-encoded image (when format is png)
	Text   string `json:"text,omitempty"`  // plain text (when format is text)
}

// ClipboardWriteResponse is the JSON response for POST /clipboard on success.
type ClipboardWriteResponse struct {
	OK        bool   `json:"ok"`
	Format    string `json:"format"`
	ByteCount int64  `json:"byte_count"`
}

// ClipboardAnalysis carries optional rich context for images produced by the
// VisionPaste analysis pipeline (OCR text, natural language description).
// Populated only for png responses when vision analysis is configured and succeeds.
type ClipboardAnalysis struct {
	OCRText     string `json:"ocr_text,omitempty"`
	Description string `json:"description,omitempty"`
}

// ErrorResponse is the JSON response for any error.
type ErrorResponse struct {
	OK      bool   `json:"ok"`
	Code    string `json:"code"`
	Error   string `json:"error"`
	FixHint string `json:"fix_hint"`
}

// VersionResponse is the JSON response for GET /version.
type VersionResponse struct {
	OK              bool   `json:"ok"`
	ProtocolVersion int    `json:"protocol_version"`
	BinaryVersion   string `json:"binary_version"`
	// Watch-related status (populated by daemon when clipboard watching is enabled).
	WatchEnabled        bool   `json:"watch_enabled"`
	LastClipboardChange string `json:"last_clipboard_change,omitempty"`
	// Relay (v1.0) status for TUI + doctor (populated when [relay] enabled in config).
	Relay *RelayInfo `json:"relay,omitempty"`
	// Recall v2 status (lightweight scalars for TUI/doctor; populated when [recall] enabled).
	RecallEnabled bool   `json:"recall_enabled,omitempty"`
	RecallDim     int    `json:"recall_dim,omitempty"`
	RecallStatus  string `json:"recall_status,omitempty"` // "ready", "ready (384d)", etc.
}

// RelayInfo mirrors the plan-specified sub-object for live relay dashboard/doctor data.
type RelayInfo struct {
	Enabled     bool   `json:"enabled"`
	DeviceID    string `json:"device_id,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	PeerCount   int    `json:"peer_count"`
	LastPush    string `json:"last_push,omitempty"`
	RelayURL    string `json:"relay_url,omitempty"`
	Healthy     bool   `json:"healthy"`
}

// HealthResponse is the JSON response for GET /health.
type HealthResponse struct {
	OK bool `json:"ok"`
}

// HistoryEntry represents a single clipboard history entry.
type HistoryEntry struct {
	ID         string `json:"id"`
	CapturedAt string `json:"captured_at"`
	Format     string `json:"format"`
	ByteCount  int64  `json:"byte_count"`
	Hash       string `json:"hash"`
}

// HistoryResponse is the JSON response for GET /clipboard/history.
type HistoryResponse struct {
	OK    bool           `json:"ok"`
	Items []HistoryEntry `json:"items"`
}

// SearchResult is a single ranked result from Recall semantic search
// (GET /clipboard/history/search). Score is cosine similarity (higher = better).
// Preview is a short excerpt of the embedded text (content or VisionPaste analysis).
type SearchResult struct {
	ID         string  `json:"id"`
	Score      float64 `json:"score"`
	CapturedAt string  `json:"captured_at"`
	Format     string  `json:"format"`
	Preview    string  `json:"preview,omitempty"`
}

// SearchResponse is the JSON response for semantic history search.
type SearchResponse struct {
	OK      bool           `json:"ok"`
	Results []SearchResult `json:"results"`
	Query   string         `json:"query,omitempty"`
}

// WatchNotification is the JSON payload pushed over the WebSocket.
type WatchNotification struct {
	Event      string `json:"event"` // "clipboard_changed"
	CapturedAt string `json:"captured_at"`
	Format     string `json:"format"`
	ID         string `json:"id,omitempty"`
}

// SnippetEntry represents a snippet in the list (metadata only).
type SnippetEntry struct {
	Name        string `json:"name"`
	Format      string `json:"format"`
	Size        int64  `json:"size"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
	Description string `json:"description,omitempty"`
	Hash        string `json:"hash"`
}

// SnippetListResponse is the JSON response for GET /snippets.
type SnippetListResponse struct {
	OK    bool           `json:"ok"`
	Items []SnippetEntry `json:"items"`
}

// SnippetSaveRequest is the JSON request for POST /snippets.
type SnippetSaveRequest struct {
	Name        string `json:"name"`
	Format      string `json:"format"` // "text" or "png"
	Text        string `json:"text,omitempty"`
	Image       string `json:"image,omitempty"` // base64
	Description string `json:"description,omitempty"`
}

// SnippetSaveResponse is the JSON response for POST /snippets.
type SnippetSaveResponse struct {
	OK      bool   `json:"ok"`
	Name    string `json:"name"`
	Created bool   `json:"created"` // true if new, false if updated
}

// SnippetDeleteResponse is the JSON response for DELETE /snippets/{name}.
type SnippetDeleteResponse struct {
	OK   bool   `json:"ok"`
	Name string `json:"name"`
}
