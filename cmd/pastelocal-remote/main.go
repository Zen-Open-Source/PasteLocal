package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pastelocal/pastelocal/internal/errors"
	"github.com/pastelocal/pastelocal/internal/proto"
)

var randChars = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

func main() {
	port := flag.Int("port", 7331, "daemon port")
	outDir := flag.String("out", "~/.cache/pastelocal", "output directory")
	flag.Bool("keep", false, "keep file after read (don't auto-delete; only affects skill behavior)")
	timeout := flag.Duration("timeout", 5*time.Second, "request timeout")
	tokenFile := flag.String("token-file", "~/.config/pastelocal/token", "path to token file")

	// Send mode: push a file from remote to local clipboard.
	send := flag.String("send", "", "send a file to the local clipboard (path to image or text file)")
	sendFormat := flag.String("send-format", "", "format for send: png or text (auto-detected if empty)")

	// Watch mode: connect to WebSocket and print notifications.
	watch := flag.Bool("watch", false, "watch for clipboard changes via WebSocket")

	flag.Parse()

	os.Exit(run(*port, expandHome(*outDir), *timeout, expandHome(*tokenFile), *send, *sendFormat, *watch))
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}

// readToken reads and trims the token from the given file path.
func readToken(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading token file %s: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}

func run(port int, outDir string, timeout time.Duration, tokenFile string, sendPath string, sendFormat string, doWatch bool) int {
	token, err := readToken(tokenFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading token: %v\n", err)
		return 10
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: timeout}

	// Watch mode.
	if doWatch {
		return runWatch(baseURL, token)
	}

	// Send mode: push a file to the local clipboard.
	if sendPath != "" {
		return runSend(client, baseURL, token, sendPath, sendFormat)
	}

	// Default: fetch clipboard (read mode).
	// Check protocol version on first call.
	if code := checkVersion(client, baseURL, token); code != 0 {
		return code
	}

	// Fetch clipboard content.
	req, err := http.NewRequest(http.MethodGet, baseURL+"/clipboard", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating request: %v\n", redact(err.Error(), token))
		return 10
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		if isConnectionRefused(err) {
			fmt.Fprintf(os.Stderr, "tunnel not connected: connection refused\n")
			return 1
		}
		fmt.Fprintf(os.Stderr, "error fetching clipboard: %v\n", redact(err.Error(), token))
		return 10
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return handleErrorResponse(resp)
	}

	// Decode success response.
	var clipResp proto.ClipboardResponse
	if err := json.NewDecoder(resp.Body).Decode(&clipResp); err != nil {
		fmt.Fprintf(os.Stderr, "error decoding response: %v\n", err)
		return 10
	}

	// Handle different formats.
	switch clipResp.Format {
	case "png", "jpeg", "gif":
		// Decode base64 image.
		imgData, err := base64.StdEncoding.DecodeString(clipResp.Image)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error decoding base64 image: %v\n", err)
			return 4
		}
		path, err := writeFile(imgData, clipResp.Format, outDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error writing image: %v\n", err)
			return 10
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error resolving absolute path: %v\n", err)
			return 10
		}
		fmt.Println(absPath)

	case "text":
		// Write text to file.
		ext := "txt"
		path, err := writeFile([]byte(clipResp.Text), ext, outDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error writing text: %v\n", err)
			return 10
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error resolving absolute path: %v\n", err)
			return 10
		}
		fmt.Println(absPath)

	default:
		fmt.Fprintf(os.Stderr, "unknown clipboard format: %s\n", clipResp.Format)
		return 4
	}

	return 0
}

// runSend pushes a local file to the remote clipboard via POST /clipboard.
func runSend(client *http.Client, baseURL, token, filePath, format string) int {
	// Read the file.
	data, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading file %s: %v\n", filePath, err)
		return 10
	}

	// Auto-detect format if not specified.
	if format == "" {
		format = detectFormat(filePath, data)
	}

	// Build request.
	reqBody := proto.ClipboardWriteRequest{Format: format}
	switch format {
	case "png":
		reqBody.Image = base64.StdEncoding.EncodeToString(data)
	case "text":
		reqBody.Text = string(data)
	default:
		fmt.Fprintf(os.Stderr, "unsupported format for send: %s\n", format)
		return 4
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error encoding request: %v\n", err)
		return 10
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/clipboard", strings.NewReader(string(bodyBytes)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating request: %v\n", redact(err.Error(), token))
		return 10
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		if isConnectionRefused(err) {
			fmt.Fprintf(os.Stderr, "tunnel not connected: connection refused\n")
			return 1
		}
		fmt.Fprintf(os.Stderr, "error sending to clipboard: %v\n", redact(err.Error(), token))
		return 10
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return handleErrorResponse(resp)
	}

	var writeResp proto.ClipboardWriteResponse
	if err := json.NewDecoder(resp.Body).Decode(&writeResp); err != nil {
		fmt.Fprintf(os.Stderr, "error decoding response: %v\n", err)
		return 10
	}

	if !writeResp.OK {
		fmt.Fprintf(os.Stderr, "clipboard write failed\n")
		return 10
	}

	fmt.Fprintf(os.Stderr, "Sent %s (%d bytes) to local clipboard\n", writeResp.Format, writeResp.ByteCount)
	return 0
}

// runWatch connects to the WebSocket endpoint and prints notifications.
func runWatch(baseURL, token string) int {
	// For WebSocket, we need to use a WebSocket client.
	// Convert http:// to ws://.
	wsURL := strings.Replace(baseURL, "http://", "ws://", 1) + "/clipboard/watch?token=" + token

	fmt.Fprintf(os.Stderr, "Connecting to %s ...\n", redact(wsURL, token))

	// Simple HTTP-based watch: poll /clipboard for changes.
	// Full WebSocket support would require a WebSocket client library.
	// For now, implement a polling-based watch that checks every second.
	lastHash := ""
	client := &http.Client{Timeout: 3 * time.Second}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		req, err := http.NewRequest(http.MethodGet, baseURL+"/clipboard", nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error creating request: %v\n", err)
			continue
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := client.Do(req)
		if err != nil {
			if isConnectionRefused(err) {
				fmt.Fprintf(os.Stderr, "tunnel not connected\n")
				return 1
			}
			continue
		}

		var clipResp proto.ClipboardResponse
		if err := json.NewDecoder(resp.Body).Decode(&clipResp); err != nil {
			resp.Body.Close()
			continue
		}
		resp.Body.Close()

		if !clipResp.OK {
			continue
		}

		// Compute hash of content to detect changes.
		var contentHash string
		if clipResp.Format == "text" {
			contentHash = fmt.Sprintf("%x", len(clipResp.Text))
		} else {
			contentHash = fmt.Sprintf("%x-%d", clipResp.ByteCount, clipResp.CapturedAt)
		}

		if contentHash != lastHash && lastHash != "" {
			fmt.Printf("clipboard_changed format=%s captured_at=%s\n",
				clipResp.Format, clipResp.CapturedAt)
		}
		lastHash = contentHash
	}

	return 0
}

// detectFormat auto-detects the format of a file based on extension and content.
func detectFormat(path string, data []byte) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".tiff", ".heic":
		return "png" // we send images as png base64
	case ".txt", ".md", ".json", ".yaml", ".yml", ".toml", ".csv", ".log", ".sh", ".py", ".go", ".rs", ".js", ".ts":
		return "text"
	default:
		// Check magic bytes.
		if len(data) >= 8 && data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
			return "png"
		}
		// Default to text.
		return "text"
	}
}

// checkVersion calls GET /version and validates the protocol version.
func checkVersion(client *http.Client, baseURL, token string) int {
	req, err := http.NewRequest(http.MethodGet, baseURL+"/version", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating version request: %v\n", err)
		return 10
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		if isConnectionRefused(err) {
			fmt.Fprintf(os.Stderr, "tunnel not connected: connection refused\n")
			return 1
		}
		fmt.Fprintf(os.Stderr, "error checking version: %v\n", redact(err.Error(), token))
		return 10
	}
	defer resp.Body.Close()

	// If the version endpoint is unavailable, proceed anyway.
	if resp.StatusCode != http.StatusOK {
		return 0
	}

	var verResp proto.VersionResponse
	if err := json.NewDecoder(resp.Body).Decode(&verResp); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not decode version response: %v\n", err)
		return 0
	}

	if verResp.ProtocolVersion == proto.ProtocolVersion {
		return 0
	}

	if verResp.ProtocolVersion > proto.ProtocolVersion {
		fmt.Fprintf(os.Stderr, "warning: protocol version mismatch (server=%d, client=%d)\n",
			verResp.ProtocolVersion, proto.ProtocolVersion)
		return 0
	}

	tmpl := errors.New("CB3001")
	fmt.Fprintf(os.Stderr, "%s: %s\n%s\n", tmpl.Code, tmpl.Message, tmpl.FixHint)
	return 10
}

// isConnectionRefused returns true if the error indicates a refused TCP connection.
func isConnectionRefused(err error) bool {
	return strings.Contains(err.Error(), "connection refused")
}

// redact removes the token from a string so it never appears in log output.
func redact(s, token string) string {
	if token == "" {
		return s
	}
	return strings.ReplaceAll(s, token, "***")
}

// handleErrorResponse decodes a JSON error from the server and maps it to the
// appropriate exit code.
func handleErrorResponse(resp *http.Response) int {
	var errResp proto.ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		fmt.Fprintf(os.Stderr, "error: HTTP %d (could not decode error response)\n", resp.StatusCode)
		return 10
	}

	fmt.Fprintf(os.Stderr, "%s: %s\n%s\n", errResp.Code, errResp.Error, errResp.FixHint)

	switch errResp.Code {
	case "CB2001", "CB2002":
		return 2
	case "CB2003":
		return 2
	case "CB1001":
		return 3
	case "CB1004":
		return 4
	case "CB1005":
		return 5
	case "CB1008":
		return 4
	case "CB1010":
		return 6
	default:
		return 10
	}
}

// writeFile creates the output directory and writes the decoded bytes to a file.
func writeFile(data []byte, ext, outDir string) (string, error) {
	if err := os.MkdirAll(outDir, 0700); err != nil {
		return "", fmt.Errorf("creating output directory: %w", err)
	}

	if ext == "" {
		ext = "png"
	}

	ts := time.Now().Unix()
	rand6 := randomString(6)
	filename := fmt.Sprintf("pastelocal-%d-%s.%s", ts, rand6, ext)
	path := filepath.Join(outDir, filename)

	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", fmt.Errorf("writing file: %w", err)
	}

	return path, nil
}

// randomString returns n random alphanumeric characters.
func randomString(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = randChars[rand.Intn(len(randChars))]
	}
	return string(b)
}
