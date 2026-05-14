//go:build !darwin && !linux

package clipboard

import (
	cliperr "github.com/pastelocal/pastelocal/internal/errors"
	"context"
)

// unsupportedReader is returned on platforms without a clipboard tool.
type unsupportedReader struct{}

func newPlatformReader(toolOverride string) Reader {
	return &unsupportedReader{}
}

// ReadImage always returns an error on unsupported platforms.
func (r *unsupportedReader) ReadImage(ctx context.Context) ([]byte, error) {
	return nil, cliperr.NewWithMessage("CB1002",
		"clipboard reading is not supported on this platform")
}

// ReadContent always returns an error on unsupported platforms.
func (r *unsupportedReader) ReadContent(ctx context.Context) (*Content, error) {
	return nil, cliperr.NewWithMessage("CB1002",
		"clipboard reading is not supported on this platform")
}

// ReadText always returns an error on unsupported platforms.
func (r *unsupportedReader) ReadText(ctx context.Context) (string, error) {
	return "", cliperr.NewWithMessage("CB1002",
		"clipboard reading is not supported on this platform")
}

// AvailableFormats always returns an error on unsupported platforms.
func (r *unsupportedReader) AvailableFormats(ctx context.Context) ([]string, error) {
	return nil, cliperr.NewWithMessage("CB1002",
		"clipboard reading is not supported on this platform")
}
