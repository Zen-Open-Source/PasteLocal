//go:build darwin

package clipboard

import (
	"bytes"
	"context"
	"os/exec"

	cliperr "github.com/pastelocal/pastelocal/internal/errors"
)

// macOSWriter writes data to the clipboard using pbcopy and osascript.
type macOSWriter struct{}

func newPlatformWriter(toolOverride string) Writer {
	return &macOSWriter{}
}

// WriteImage writes PNG bytes to the clipboard using osascript.
func (w *macOSWriter) WriteImage(ctx context.Context, data []byte) error {
	// Write image data to a temp file and use osascript to set clipboard.
	// Alternatively, use pbcopy with the right encoding.
	// The most reliable approach on macOS is to write to a temp file
	// and use osascript to set the clipboard to the file contents.
	return writeImageViaTempFile(ctx, data)
}

// WriteText writes a plain text string to the clipboard via pbcopy.
func (w *macOSWriter) WriteText(ctx context.Context, text string) error {
	cmd := exec.CommandContext(ctx, "/usr/bin/pbcopy")
	cmd.Stdin = bytes.NewBufferString(text)
	if err := cmd.Run(); err != nil {
		return cliperr.NewWithMessage("CB1006",
			"pbcopy failed: "+err.Error())
	}
	return nil
}

// Write writes content with the specified format to the clipboard.
func (w *macOSWriter) Write(ctx context.Context, content *Content) error {
	switch content.Format {
	case "text":
		return w.WriteText(ctx, string(content.Data))
	case "png":
		return w.WriteImage(ctx, content.Data)
	default:
		return cliperr.NewWithMessage("CB1008",
			"unsupported clipboard format for writing: "+content.Format)
	}
}

// writeImageViaTempFile writes image data to the clipboard by saving
// to a temp file and using osascript to read it into the clipboard.
func writeImageViaTempFile(ctx context.Context, data []byte) error {
	// Write to a temp file.
	path, cleanup, err := tempFile(data, ".png")
	if err != nil {
		return cliperr.NewWithMessage("CB1006",
			"creating temp file: "+err.Error())
	}
	defer cleanup()

	// Use osascript to set the clipboard to the image file.
	script := `
set theFile to POSIX file "` + path + `"
set theClipboard to (read theFile as «class PNGf»)
set the clipboard to theClipboard
`
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return cliperr.NewWithMessage("CB1006",
			"osascript set clipboard failed: "+err.Error()+": "+string(out))
	}
	return nil
}
