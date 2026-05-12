//go:build darwin

package clipboard

import (
	"bytes"
	"context"
	"os/exec"

	cliperr "github.com/pastelocal/pastelocal/internal/errors"
)

const defaultPngpaste = "pngpaste"

// macOSReader reads images from the clipboard using pngpaste.
type macOSReader struct {
	pngpastePath string
}

func newPlatformReader(toolOverride string) Reader {
	path := defaultPngpaste
	if toolOverride != "" {
		path = toolOverride
	}
	return &macOSReader{pngpastePath: path}
}

// ReadImage reads the clipboard via pngpaste and returns PNG bytes.
func (r *macOSReader) ReadImage(ctx context.Context) ([]byte, error) {
	// Check that pngpaste is available.
	resolved, err := exec.LookPath(r.pngpastePath)
	if err != nil {
		return nil, cliperr.NewWithMessage("CB1002",
			"pngpaste not found on PATH; install with `brew install pngpaste`")
	}

	cmd := exec.CommandContext(ctx, resolved, "-")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// pngpaste exits with code 1 when no image data is on the clipboard.
		if exitErr, ok := err.(*exec.ExitError); ok {
			switch exitErr.ExitCode() {
			case 1:
				return nil, cliperr.New("CB1001")
			default:
				return nil, cliperr.NewWithMessage("CB1003",
					"pngpaste failed: "+stderr.String())
			}
		}
		return nil, cliperr.NewWithMessage("CB1003",
			"pngpaste failed: "+err.Error())
	}

	data := stdout.Bytes()
	if len(data) == 0 {
		return nil, cliperr.New("CB1001")
	}

	// If the data is already PNG, return it directly.
	if isPNG(data) {
		return data, nil
	}

	// Otherwise convert to PNG.
	pngData, convErr := ConvertToPNG(data)
	if convErr != nil {
		return nil, cliperr.NewWithMessage("CB1004", convErr.Error())
	}
	return pngData, nil
}
