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

	"github.com/clipbridge/clipbridge/internal/errors"
	"github.com/clipbridge/clipbridge/internal/proto"
)

var randChars = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

func main() {
	port := flag.Int("port", 7331, "daemon port")
	outDir := flag.String("out", "~/.cache/clipbridge", "output directory")
	flag.Bool("keep", false, "keep file after read (don't auto-delete; only affects skill behavior)")
	timeout := flag.Duration("timeout", 5*time.Second, "request timeout")
	tokenFile := flag.String("token-file", "~/.config/clipbridge/token", "path to token file")
	flag.Parse()

	os.Exit(run(*port, expandHome(*outDir), *timeout, expandHome(*tokenFile)))
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

func run(port int, outDir string, timeout time.Duration, tokenFile string) int {
	token, err := readToken(tokenFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading token: %v\n", err)
		return 10
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: timeout}

	// Check protocol version on first call.
	if code := checkVersion(client, baseURL, token); code != 0 {
		return code
	}

	// Fetch clipboard image.
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

	// Base64-decode the image.
	imgData, err := base64.StdEncoding.DecodeString(clipResp.Image)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error decoding base64 image: %v\n", err)
		return 4
	}

	// Write image to disk.
	path, err := writeImage(imgData, clipResp.Format, outDir)
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
	return 0
}

// checkVersion calls GET /version and validates the protocol version.
// Warns on minor mismatch (server newer), fails on major mismatch (server older).
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

	// If the version endpoint is unavailable, proceed anyway — older servers
	// may not expose it.
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

	// Server has a higher version: likely backward-compatible — warn only.
	if verResp.ProtocolVersion > proto.ProtocolVersion {
		fmt.Fprintf(os.Stderr, "warning: protocol version mismatch (server=%d, client=%d)\n",
			verResp.ProtocolVersion, proto.ProtocolVersion)
		return 0
	}

	// Server has an older version: likely incompatible — fail.
	tmpl := errors.New("CB3001")
	fmt.Fprintf(os.Stderr, "%s: %s\n%s\n", tmpl.Code, tmpl.Message, tmpl.FixHint)
	return 10
}

// isConnectionRefused returns true if the error indicates a refused TCP
// connection (the SSH tunnel is not established).
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
// appropriate exit code. Output format: "CODE: message\nfix_hint\n".
func handleErrorResponse(resp *http.Response) int {
	var errResp proto.ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		fmt.Fprintf(os.Stderr, "error: HTTP %d (could not decode error response)\n", resp.StatusCode)
		return 10
	}

	fmt.Fprintf(os.Stderr, "%s: %s\n%s\n", errResp.Code, errResp.Error, errResp.FixHint)

	// Map well-known error codes to exit codes per spec §5.2.
	switch errResp.Code {
	case "CB2001", "CB2002":
		return 2 // auth failure
	case "CB1001":
		return 3 // no image on clipboard
	case "CB1004":
		return 4 // image conversion / format error
	case "CB1005":
		return 5 // image too large
	default:
		return 10 // any other error
	}
}

// writeImage creates the output directory (mode 0700) and writes the decoded
// image bytes to a file named clipbridge-<unix_ts>-<rand6>.<ext> (mode 0600).
func writeImage(data []byte, format, outDir string) (string, error) {
	if err := os.MkdirAll(outDir, 0700); err != nil {
		return "", fmt.Errorf("creating output directory: %w", err)
	}

	ext := format
	if ext == "" {
		ext = "png"
	}

	ts := time.Now().Unix()
	rand6 := randomString(6)
	filename := fmt.Sprintf("clipbridge-%d-%s.%s", ts, rand6, ext)
	path := filepath.Join(outDir, filename)

	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", fmt.Errorf("writing image file: %w", err)
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
