package clipboard

import "context"

// Content represents data read from the OS clipboard, carrying both
// the raw bytes and the detected format.
type Content struct {
	Data   []byte
	Format string // "png", "text", or "html"
}

// Reader reads data from the OS clipboard.
type Reader interface {
	// ReadImage reads the clipboard and returns PNG bytes.
	// Returns an error with code CB1001 if no image on clipboard.
	// Returns an error with code CB1002 if the clipboard tool is not installed.
	// Returns an error with code CB1003 if the clipboard tool fails.
	// Returns an error with code CB1004 if image conversion fails.
	ReadImage(ctx context.Context) ([]byte, error)

	// ReadContent reads the clipboard and returns a Content with the
	// detected format. It tries image first, then text.
	ReadContent(ctx context.Context) (*Content, error)

	// ReadText reads the clipboard as plain text.
	// Returns an error with code CB1001 if no text on clipboard.
	ReadText(ctx context.Context) (string, error)

	// AvailableFormats returns the list of MIME types currently on the clipboard.
	AvailableFormats(ctx context.Context) ([]string, error)

	// IsConcealed reports whether the current clipboard item is marked sensitive
	// by the source application (e.g. via org.nspasteboard.ConcealedType on macOS
	// for items copied from 1Password and other password managers). When true,
	// callers should avoid relaying or returning the content.
	//
	// The error return is used only to signal that detection itself failed
	// (e.g. osascript error on macOS). In that case the bool value must be
	// ignored and the item treated as non-concealed (fail-open for availability).
	//
	// This accepts the (rare) risk of a false negative — a secret could be
	// relayed — which is the explicit trade-off documented in the spec's
	// threat model ("false negatives worse than false positives; err on the
	// side of caution"). Callers log the failure (including the wrapped error
	// that contains any stderr) at Info when log_filtered_items is true,
	// otherwise Debug, making the condition fully observable and auditable.
	IsConcealed(ctx context.Context) (bool, error)
}

// NewReader returns the appropriate platform Reader.
// If toolOverride is non-empty, that tool path is used instead of
// auto-detecting the platform clipboard tool.
func NewReader(toolOverride string) Reader {
	return newPlatformReader(toolOverride)
}
