//go:build linux && x11

package clipboard

import (
	"bytes"
	"context"
	"os/exec"
	"strings"

	cliperr "github.com/pastelocal/pastelocal/internal/errors"
)

const defaultXClip = "xclip"

// X11Reader reads images and text from the clipboard using xclip.
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
func (r *X11Reader) ReadContent(ctx context.Context) (*Content, error) {
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

// ReadText reads the clipboard as plain text via xclip.
func (r *X11Reader) ReadText(ctx context.Context) (string, error) {
	resolved, err := exec.LookPath(r.clipboardTool)
	if err != nil {
		return "", cliperr.NewWithMessage("CB1002",
			"xclip not found on PATH; install with your distro's package manager")
	}

	cmd := exec.CommandContext(ctx, resolved,
		"-selection", "clipboard",
		"-t", "text/plain",
		"-o",
	)
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
					"xclip failed: "+stderr.String())
			}
		}
		return "", cliperr.NewWithMessage("CB1003",
			"xclip failed: "+err.Error())
	}

	text := stdout.String()
	if len(text) == 0 {
		return "", cliperr.New("CB1001")
	}
	return text, nil
}

// AvailableFormats returns the list of targets (MIME types) currently on the clipboard.
func (r *X11Reader) AvailableFormats(ctx context.Context) ([]string, error) {
	resolved, err := exec.LookPath(r.clipboardTool)
	if err != nil {
		return nil, cliperr.NewWithMessage("CB1002",
			"xclip not found on PATH")
	}

	cmd := exec.CommandContext(ctx, resolved,
		"-selection", "clipboard",
		"-t", "TARGETS",
		"-o",
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, cliperr.NewWithMessage("CB1003",
			"xclip TARGETS failed: "+err.Error())
	}

	output := strings.TrimSpace(stdout.String())
	if output == "" {
		return []string{}, nil
	}
	return strings.Split(output, "\n"), nil
}
