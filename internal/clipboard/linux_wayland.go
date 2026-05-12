//go:build linux && wayland

package clipboard

import (
	"bytes"
	"context"
	"os/exec"

	cliperr "github.com/clipbridge/clipbridge/internal/errors"
)

const defaultWLPaste = "wl-paste"

// WaylandReader reads images from the clipboard using wl-paste.
type WaylandReader struct {
	clipboardTool string
}

func newPlatformReader(toolOverride string) Reader {
	tool := defaultWLPaste
	if toolOverride != "" {
		tool = toolOverride
	}
	return &WaylandReader{clipboardTool: tool}
}

// ReadImage reads the clipboard via wl-paste and returns PNG bytes.
func (r *WaylandReader) ReadImage(ctx context.Context) ([]byte, error) {
	// Check that wl-paste is available.
	resolved, err := exec.LookPath(r.clipboardTool)
	if err != nil {
		return nil, cliperr.NewWithMessage("CB1002",
			"wl-paste not found on PATH; install with your distro's package manager")
	}

	cmd := exec.CommandContext(ctx, resolved, "--type", "image/png")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// wl-paste exits non-zero when no image is on the clipboard or
		// when the requested MIME type is unavailable.
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Common exit code 1 indicates no matching content.
			switch exitErr.ExitCode() {
			case 1:
				return nil, cliperr.New("CB1001")
			default:
				return nil, cliperr.NewWithMessage("CB1003",
					"wl-paste failed: "+stderr.String())
			}
		}
		return nil, cliperr.NewWithMessage("CB1003",
			"wl-paste failed: "+err.Error())
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
