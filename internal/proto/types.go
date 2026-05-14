package proto

// ProtocolVersion is the current wire protocol version.
const ProtocolVersion = 2

// ClipboardResponse is the JSON response for GET /clipboard on success.
type ClipboardResponse struct {
	OK         bool   `json:"ok"`
	Image      string `json:"image,omitempty"`       // base64-encoded image (png/jpeg/etc)
	Text       string `json:"text,omitempty"`         // plain text content (when format is text)
	Format     string `json:"format"`                 // "png", "text", "html"
	ByteCount  int64  `json:"byte_count"`
	CapturedAt string `json:"captured_at"`            // RFC3339
	ID         string `json:"id,omitempty"`            // unique ID for history entries
}

// ClipboardWriteRequest is the JSON request for POST /clipboard.
type ClipboardWriteRequest struct {
	Format string `json:"format"`           // "png" or "text"
	Image  string `json:"image,omitempty"`  // base64-encoded image (when format is png)
	Text   string `json:"text,omitempty"`   // plain text (when format is text)
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
	OK     bool            `json:"ok"`
	Items  []HistoryEntry  `json:"items"`
}

// WatchNotification is the JSON payload pushed over the WebSocket.
type WatchNotification struct {
	Event      string `json:"event"`       // "clipboard_changed"
	CapturedAt string `json:"captured_at"`
	Format     string `json:"format"`
	ID         string `json:"id,omitempty"`
}
