package clipboard

import "context"

// Writer writes data to the OS clipboard.
type Writer interface {
	// WriteImage writes PNG bytes to the clipboard.
	WriteImage(ctx context.Context, data []byte) error

	// WriteText writes a plain text string to the clipboard.
	WriteText(ctx context.Context, text string) error

	// Write writes content with the specified format to the clipboard.
	Write(ctx context.Context, content *Content) error
}

// NewWriter returns the appropriate platform Writer.
func NewWriter(toolOverride string) Writer {
	return newPlatformWriter(toolOverride)
}
