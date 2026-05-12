package proto

// ProtocolVersion is the current wire protocol version.
const ProtocolVersion = 1

// ClipboardResponse is the JSON response for GET /clipboard on success.
type ClipboardResponse struct {
	OK         bool   `json:"ok"`
	Image      string `json:"image"`       // base64-encoded PNG
	Format     string `json:"format"`      // "png"
	ByteCount  int64  `json:"byte_count"`
	CapturedAt string `json:"captured_at"` // RFC3339
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
