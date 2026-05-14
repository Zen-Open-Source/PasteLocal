//go:build linux && wayland

package clipboard

import (
	"bytes"
	"context"
	"os/exec"
	"strings"

	cliperr "github.com/pastelocal/pastelocal/internal/errors"
)

const defaultWLPaste = "wl-paste"

// WaylandReader reads images and text from the clipboard using wl-paste.
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
		if exitErr, ok := err.(*exec.ExitError); ok {
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

	if isPNG(data) {
		return data, nil
	}

	pngData, convErr := ConvertToPNG(data)
	if convErr != nil {
		return nil, cliperr.NewWithMessage("CB1004", convErr.Error())
	}
	return pngData, nil
}

// ReadContent reads the clipboard and returns a Content with the detected format.
func (r *WaylandReader) ReadContent(ctx context.Context) (*Content, error) {
	imgData, err := r.ReadImage(ctx)
	if err == nil {
		return &Content{Data: imgData, Format: "png"}, nil
	}
	if clipErr, ok := err.(*cliperr.Error); ok && clipErr.Code == "CB1001" {
		text, textErr := r.ReadText(ctx)
		if textErr == nil {
			return &Content{Data: []byte(text), Format: "text"}, nil
		}
		return nil, cliperr.New("CB1001")
	}
	return nil, err
}

// ReadText reads the clipboard as plain text via wl-paste.
func (r *WaylandReader) ReadText(ctx context.Context) (string, error) {
	resolved, err := exec.LookPath(r.clipboardTool)
	if err != nil {
		return "", cliperr.NewWithMessage("CB1002",
			"wl-paste not found on PATH; install with your distro's package manager")
	}

	cmd := exec.CommandContext(ctx, resolved, "--type", "text/plain")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			switch exitErr.ExitCode() {
			case 1:
				return "", cliperr.New("CB1001")
			default:
				return "", cliperr.NewWithMessage("CB1003",
					"wl-paste failed: "+stderr.String())
			}
		}
		return "", cliperr.NewWithMessage("CB1003",
			"wl-paste failed: "+err.Error())
	}

	text := stdout.String()
	if len(text) == 0 {
		return "", cliperr.New("CB1001")
	}
	return text, nil
}

// AvailableFormats returns the list of MIME types currently on the clipboard.
func (r *WaylandReader) AvailableFormats(ctx context.Context) ([]string, error) {
	resolved, err := exec.LookPath(r.clipboardTool)
	if err != nil {
		return nil, cliperr.NewWithMessage("CB1002",
			"wl-paste not found on PATH")
	}

	cmd := exec.CommandContext(ctx, resolved, "-l")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, cliperr.NewWithMessage("CB1003",
			"wl-paste -l failed: "+err.Error())
	}

	output := strings.TrimSpace(stdout.String())
	if output == "" {
		return []string{}, nil
	}
	return strings.Split(output, "\n"), nil
}
