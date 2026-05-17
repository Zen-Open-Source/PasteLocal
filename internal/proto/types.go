package proto

// ProtocolVersion is the current wire protocol version.
const ProtocolVersion = 2

// ClipboardResponse is the JSON response for GET /clipboard on success.
type ClipboardResponse struct {
	OK         bool   `json:"ok"`
	Image      string `json:"image,omitempty"` // base64-encoded image (png/jpeg/etc)
	Text       string `json:"text,omitempty"`  // plain text content (when format is text)
	Format     string `json:"format"`          // "png", "text", "html"
	ByteCount  int64  `json:"byte_count"`
	CapturedAt string `json:"captured_at"`  // RFC3339
	ID         string `json:"id,omitempty"` // unique ID for history entries
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
