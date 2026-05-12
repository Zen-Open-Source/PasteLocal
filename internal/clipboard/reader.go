package clipboard

import "context"

// Reader reads image data from the OS clipboard.
type Reader interface {
	// ReadImage reads the clipboard and returns PNG bytes.
	// Returns an error with code CB1001 if no image on clipboard.
	// Returns an error with code CB1002 if the clipboard tool is not installed.
	// Returns an error with code CB1003 if the clipboard tool fails.
	// Returns an error with code CB1004 if image conversion fails.
	ReadImage(ctx context.Context) ([]byte, error)
}

// NewReader returns the appropriate platform Reader.
// If toolOverride is non-empty, that tool path is used instead of
// auto-detecting the platform clipboard tool.
func NewReader(toolOverride string) Reader {
	return newPlatformReader(toolOverride)
}
