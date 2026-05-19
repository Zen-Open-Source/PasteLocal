//go:build linux && !wayland && !x11

package clipboard

import (
	"context"

	cliperr "github.com/pastelocal/pastelocal/internal/errors"
)

// linuxDefaultReader is returned on Linux when neither wayland nor x11
// build tags are specified.
type linuxDefaultReader struct{}

func newPlatformReader(toolOverride string) Reader {
	return &linuxDefaultReader{}
}

// ReadImage always returns an error when no display server tag is set.
func (r *linuxDefaultReader) ReadImage(ctx context.Context) ([]byte, error) {
	return nil, cliperr.NewWithMessage("CB1002",
		"no display server selected; rebuild with -tags wayland or -tags x11")
}

// ReadContent always returns an error when no display server tag is set.
func (r *linuxDefaultReader) ReadContent(ctx context.Context) (*Content, error) {
	return nil, cliperr.NewWithMessage("CB1002",
		"no display server selected; rebuild with -tags wayland or -tags x11")
}

// ReadText always returns an error when no display server tag is set.
func (r *linuxDefaultReader) ReadText(ctx context.Context) (string, error) {
	return "", cliperr.NewWithMessage("CB1002",
		"no display server selected; rebuild with -tags wayland or -tags x11")
}

// AvailableFormats always returns an error when no display server tag is set.
func (r *linuxDefaultReader) AvailableFormats(ctx context.Context) ([]string, error) {
	return nil, cliperr.NewWithMessage("CB1002",
		"no display server selected; rebuild with -tags wayland or -tags x11")
}

// IsConcealed reports false on Linux (no first-class ConcealedType equivalent;
// the regex redaction layer provides best-effort secret protection).
func (r *linuxDefaultReader) IsConcealed(ctx context.Context) (bool, error) {
	return false, nil
}
