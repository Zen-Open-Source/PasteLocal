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
