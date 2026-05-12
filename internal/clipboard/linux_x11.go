//go:build linux && x11

package clipboard

import (
	"bytes"
	"context"
	"os/exec"

	cliperr "github.com/clipbridge/clipbridge/internal/errors"
)

const defaultXClip = "xclip"

// X11Reader reads images from the clipboard using xclip.
type X11Reader struct {
	clipboardTool string
}

func newPlatformReader(toolOverride string) Reader {
	tool := defaultXClip
	if toolOverride != "" {
		tool = toolOverride
	}
	return &X11Reader{clipboardTool: tool}
}

// ReadImage reads the clipboard via xclip and returns PNG bytes.
func (r *X11Reader) ReadImage(ctx context.Context) ([]byte, error) {
	// Check that xclip is available.
	resolved, err := exec.LookPath(r.clipboardTool)
	if err != nil {
		return nil, cliperr.NewWithMessage("CB1002",
			"xclip not found on PATH; install with your distro's package manager")
	}

	cmd := exec.CommandContext(ctx, resolved,
		"-selection", "clipboard",
		"-t", "image/png",
		"-o",
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// xclip exits non-zero when no image is on the clipboard or
		// when the target atom is unavailable.
		if exitErr, ok := err.(*exec.ExitError); ok {
			switch exitErr.ExitCode() {
			case 1:
				return nil, cliperr.New("CB1001")
			default:
				return nil, cliperr.NewWithMessage("CB1003",
					"xclip failed: "+stderr.String())
			}
		}
		return nil, cliperr.NewWithMessage("CB1003",
			"xclip failed: "+err.Error())
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
