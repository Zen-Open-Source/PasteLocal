//go:build linux

package clipboard

import (
	"bytes"
	"context"
	"os/exec"

	cliperr "github.com/pastelocal/pastelocal/internal/errors"
)

// LinuxWriter writes data to the clipboard using wl-copy or xclip.
type LinuxWriter struct {
	clipboardTool string
}

func newPlatformWriter(toolOverride string) Writer {
	tool := detectClipboardTool(toolOverride)
	return &LinuxWriter{clipboardTool: tool}
}

// detectClipboardTool determines which clipboard tool to use.
func detectClipboardTool(toolOverride string) string {
	if toolOverride != "" {
		return toolOverride
	}
	// Prefer wl-copy (Wayland) over xclip (X11).
	if _, err := exec.LookPath("wl-copy"); err == nil {
		return "wl-copy"
	}
	if _, err := exec.LookPath("xclip"); err == nil {
		return "xclip"
	}
	return ""
}

// WriteImage writes PNG bytes to the clipboard.
func (w *LinuxWriter) WriteImage(ctx context.Context, data []byte) error {
	tool := w.clipboardTool
	if tool == "" {
		return cliperr.NewWithMessage("CB1002",
			"no clipboard write tool found; install wl-copy or xclip")
	}

	switch tool {
	case "wl-copy":
		cmd := exec.CommandContext(ctx, "wl-copy", "--type", "image/png")
		cmd.Stdin = bytes.NewReader(data)
		if err := cmd.Run(); err != nil {
			return cliperr.NewWithMessage("CB1006",
				"wl-copy failed: "+err.Error())
		}
		return nil
	case "xclip":
		cmd := exec.CommandContext(ctx, "xclip",
			"-selection", "clipboard",
			"-t", "image/png",
			"-i",
		)
		cmd.Stdin = bytes.NewReader(data)
		if err := cmd.Run(); err != nil {
			return cliperr.NewWithMessage("CB1006",
				"xclip failed: "+err.Error())
		}
		return nil
	default:
		return cliperr.NewWithMessage("CB1002",
			"unknown clipboard tool: "+tool)
	}
}

// WriteText writes a plain text string to the clipboard.
func (w *LinuxWriter) WriteText(ctx context.Context, text string) error {
	tool := w.clipboardTool
	if tool == "" {
		return cliperr.NewWithMessage("CB1002",
			"no clipboard write tool found; install wl-copy or xclip")
	}

	switch tool {
	case "wl-copy":
		cmd := exec.CommandContext(ctx, "wl-copy")
		cmd.Stdin = bytes.NewBufferString(text)
		if err := cmd.Run(); err != nil {
			return cliperr.NewWithMessage("CB1006",
				"wl-copy failed: "+err.Error())
		}
		return nil
	case "xclip":
		cmd := exec.CommandContext(ctx, "xclip",
			"-selection", "clipboard",
			"-i",
		)
		cmd.Stdin = bytes.NewBufferString(text)
		if err := cmd.Run(); err != nil {
			return cliperr.NewWithMessage("CB1006",
				"xclip failed: "+err.Error())
		}
		return nil
	default:
		return cliperr.NewWithMessage("CB1002",
			"unknown clipboard tool: "+tool)
	}
}

// Write writes content with the specified format to the clipboard.
func (w *LinuxWriter) Write(ctx context.Context, content *Content) error {
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
