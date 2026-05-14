//go:build darwin

package clipboard

import (
	"bytes"
	"context"
	"os/exec"
	"strings"

	cliperr "github.com/pastelocal/pastelocal/internal/errors"
)

const defaultPngpaste = "pngpaste"

// macOSReader reads images and text from the clipboard using pngpaste and pbpaste.
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
// It tries image first, then text.
func (r *macOSReader) ReadContent(ctx context.Context) (*Content, error) {
	// Try image first.
	imgData, err := r.ReadImage(ctx)
	if err == nil {
		return &Content{Data: imgData, Format: "png"}, nil
	}

	// If the error is CB1001 (no image), try text.
	if clipErr, ok := err.(*cliperr.Error); ok && clipErr.Code == "CB1001" {
		text, textErr := r.ReadText(ctx)
		if textErr == nil {
			return &Content{Data: []byte(text), Format: "text"}, nil
		}
		// Neither image nor text available.
		return nil, cliperr.New("CB1001")
	}

	// Some other clipboard error (tool missing, etc).
	return nil, err
}

// ReadText reads the clipboard as plain text via pbpaste.
func (r *macOSReader) ReadText(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "/usr/bin/pbpaste", "-Prefer", "txt")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return "", cliperr.NewWithMessage("CB1003",
			"pbpaste failed: "+err.Error())
	}

	text := stdout.String()
	if len(text) == 0 {
		return "", cliperr.New("CB1001")
	}
	return text, nil
}

// AvailableFormats returns the list of UTI types currently on the clipboard.
func (r *macOSReader) AvailableFormats(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "/usr/bin/pbpaste", "-list-utis")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return nil, cliperr.NewWithMessage("CB1003",
			"pbpaste -list-utis failed: "+err.Error())
	}

	output := strings.TrimSpace(stdout.String())
	if output == "" {
		return []string{}, nil
	}
	return strings.Split(output, "\n"), nil
}
