// Package errors defines structured error codes for pastelocal.
//
// Each error code carries an HTTP status, a human-readable message, and a
// fix hint. The package provides a registry-based lookup and a helper for
// writing JSON error responses.
package errors

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// Error represents a structured pastelocal error.
type Error struct {
	Code       string `json:"code"`
	HTTPStatus int    `json:"-"`
	Message    string `json:"error"`
	FixHint    string `json:"fix_hint"`
}

// Error implements the error interface.
func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// jsonResp is the wire format for error responses.
type jsonResp struct {
	OK      bool   `json:"ok"`
	Code    string `json:"code"`
	Error   string `json:"error"`
	FixHint string `json:"fix_hint"`
}

// Registry maps error codes to their templates.
var Registry = map[string]Error{
	"CB1001": {Code: "CB1001", HTTPStatus: http.StatusBadRequest, Message: "No image on clipboard", FixHint: "Take a screenshot first"},
	"CB1002": {Code: "CB1002", HTTPStatus: http.StatusInternalServerError, Message: "Clipboard tool not installed", FixHint: "Run `pastelocal doctor --fix`"},
	"CB1003": {Code: "CB1003", HTTPStatus: http.StatusInternalServerError, Message: "Clipboard tool failed", FixHint: "Check `pastelocal logs`"},
	"CB1004": {Code: "CB1004", HTTPStatus: http.StatusUnsupportedMediaType, Message: "Image conversion failed", FixHint: "Save as PNG manually"},
	"CB1005": {Code: "CB1005", HTTPStatus: http.StatusRequestEntityTooLarge, Message: "Image exceeds max_image_bytes", FixHint: "Raise limit in config"},
	"CB1006": {Code: "CB1006", HTTPStatus: http.StatusInternalServerError, Message: "Clipboard write failed", FixHint: "Check `pastelocal logs`"},
	"CB1007": {Code: "CB1007", HTTPStatus: http.StatusUnsupportedMediaType, Message: "Unsupported clipboard format", FixHint: "Use PNG or plain text"},
	"CB1008": {Code: "CB1008", HTTPStatus: http.StatusBadRequest, Message: "Unsupported MIME type", FixHint: "Check `allowed_formats` in config"},
	"CB1009": {Code: "CB1009", HTTPStatus: http.StatusNotFound, Message: "Clipboard entry not found", FixHint: "The history entry may have expired"},
	"CB1010": {Code: "CB1010", HTTPStatus: http.StatusForbidden, Message: "Content blocked by redaction rule", FixHint: "Clipboard content matches a redaction rule"},
	"CB1011": {Code: "CB1011", HTTPStatus: http.StatusServiceUnavailable, Message: "Processor pipeline failed", FixHint: "Check processor configuration in config"},
	"CB1012": {Code: "CB1012", HTTPStatus: http.StatusBadRequest, Message: "No text on clipboard", FixHint: "Copy some text first"},
	"CB2001": {Code: "CB2001", HTTPStatus: http.StatusUnauthorized, Message: "Invalid auth token", FixHint: "Re-run `pastelocal add-host <host>` to sync the token."},
	"CB2002": {Code: "CB2002", HTTPStatus: http.StatusUnauthorized, Message: "Missing auth token", FixHint: "Bug; report it"},
	"CB2003": {Code: "CB2003", HTTPStatus: http.StatusForbidden, Message: "Insufficient permissions", FixHint: "Token does not have the required permission"},
	"CB3001": {Code: "CB3001", HTTPStatus: http.StatusUpgradeRequired, Message: "Protocol version mismatch", FixHint: "Update local or remote binary"},
	"CB4001": {Code: "CB4001", HTTPStatus: http.StatusTooManyRequests, Message: "Rate limit exceeded", FixHint: "Wait and retry"},
	"CB4002": {Code: "CB4002", HTTPStatus: http.StatusServiceUnavailable, Message: "Too many concurrent requests", FixHint: "Wait and retry"},
}

// mu protects the Registry for concurrent reads; the registry is
// initialized once at package load time so lookups are safe without
// holding the lock, but we keep it for future extensibility.
var mu sync.RWMutex

// New looks up the given code in the registry and returns a new *Error
// with the template values. It panics if the code is not found.
func New(code string) *Error {
	mu.RLock()
	defer mu.RUnlock()

	tmpl, ok := Registry[code]
	if !ok {
		panic(fmt.Sprintf("errors: unknown error code %q", code))
	}
	e := tmpl // copy
	return &e
}

// NewWithMessage looks up the given code in the registry and returns a
// new *Error with the template values but with the message overridden.
// It panics if the code is not found.
func NewWithMessage(code string, msg string) *Error {
	e := New(code)
	e.Message = msg
	return e
}

// WriteJSON writes a JSON error response to w with the proper HTTP status.
// The response format is: {"ok": false, "code": "CB2001", "error": "...", "fix_hint": "..."}
func WriteJSON(w http.ResponseWriter, err *Error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(err.HTTPStatus)
	json.NewEncoder(w).Encode(jsonResp{
		OK:      false,
		Code:    err.Code,
		Error:   err.Message,
		FixHint: err.FixHint,
	})
}
