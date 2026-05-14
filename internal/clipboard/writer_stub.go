//go:build !darwin && !linux

package clipboard

import (
	"context"

	cliperr "github.com/pastelocal/pastelocal/internal/errors"
)

// unsupportedWriter is returned on platforms without a clipboard write tool.
type unsupportedWriter struct{}

func newPlatformWriter(toolOverride string) Writer {
	return &unsupportedWriter{}
}

// WriteImage always returns an error on unsupported platforms.
func (w *unsupportedWriter) WriteImage(ctx context.Context, data []byte) error {
	return cliperr.NewWithMessage("CB1002",
		"clipboard writing is not supported on this platform")
}

// WriteText always returns an error on unsupported platforms.
func (w *unsupportedWriter) WriteText(ctx context.Context, text string) error {
	return cliperr.NewWithMessage("CB1002",
		"clipboard writing is not supported on this platform")
}

// Write always returns an error on unsupported platforms.
func (w *unsupportedWriter) Write(ctx context.Context, content *Content) error {
	return cliperr.NewWithMessage("CB1002",
		"clipboard writing is not supported on this platform")
}
