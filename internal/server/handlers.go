package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/clipbridge/clipbridge/internal/auth"
	cliperr "github.com/clipbridge/clipbridge/internal/errors"
	"github.com/clipbridge/clipbridge/internal/proto"
)

// handleClipboard handles GET /clipboard. It reads the current clipboard image
// and returns it as a base64-encoded PNG in a proto.ClipboardResponse.
//
// The handler enforces concurrency limits (semaphore), rate limits (token bucket),
// and authentication (Bearer token) before reading the clipboard.
func (s *Server) handleClipboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Step 1: Acquire semaphore (max_in_flight).
	select {
	case <-s.sem:
		defer func() { s.sem <- struct{}{} }()
	default:
		// All slots are occupied.
		w.Header().Set("Retry-After", "1")
		cliperr.WriteJSON(w, cliperr.New("CB4001"))
		return
	}

	// Step 2: Check rate limit.
	if !s.rateLimiter.Allow() {
		w.Header().Set("Retry-After", "60")
		cliperr.WriteJSON(w, cliperr.New("CB4001"))
		return
	}

	// Step 3: Validate auth token.
	remoteIP := extractRemoteIP(r)
	token, authErr := s.validateAuth(r)
	if authErr != nil {
		s.logger.Info("auth failed",
			"auth_failed", true,
			"code", authErr.Code,
			"remote_ip", remoteIP,
		)
		cliperr.WriteJSON(w, authErr)
		return
	}

	// Step 4-6: Lock mutex, read clipboard, unlock mutex.
	s.mu.Lock()
	imgBytes, readErr := s.reader.ReadImage(r.Context())
	if readErr != nil {
		s.mu.Unlock()
		// Propagate clipboard errors as-is if they are *cliperr.Error.
		if ce, ok := readErr.(*cliperr.Error); ok {
			cliperr.WriteJSON(w, ce)
			return
		}
		cliperr.WriteJSON(w, cliperr.NewWithMessage("CB1003", readErr.Error()))
		return
	}
	// Enforce max_image_bytes limit before processing.
	if int64(len(imgBytes)) > s.cfg.MaxImageBytes {
		s.mu.Unlock()
		cliperr.WriteJSON(w, cliperr.New("CB1005"))
		return
	}

	now := time.Now().UTC()
	s.lastRead = now
	s.lastReadSize = int64(len(imgBytes))
	s.mu.Unlock()

	// Step 7-8: Encode as base64 and return.
	encoded := base64.StdEncoding.EncodeToString(imgBytes)

	resp := proto.ClipboardResponse{
		OK:         true,
		Image:      encoded,
		Format:     "png",
		ByteCount:  int64(len(imgBytes)),
		CapturedAt: now.Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)

	// Step 10: Audit logging if configured.
	if s.cfg.AuditLog != "" {
		hash := sha256.Sum256(imgBytes)
		entry := AuditEntry{
			Event:     "clipboard_read",
			ImageHash: fmt.Sprintf("%x", hash),
			ByteCount: int64(len(imgBytes)),
			SourceIP:   remoteIP,
			Auth:      token,
		}
		if err := WriteAudit(s.cfg.AuditLog, entry); err != nil {
			s.logger.Error("audit log write failed", "err", err)
		}
	}
}

// handleHealth handles GET /health. No auth required.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resp := proto.HealthResponse{OK: true}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleVersion handles GET /version. No auth required.
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resp := proto.VersionResponse{
		OK:              true,
		ProtocolVersion: proto.ProtocolVersion,
		BinaryVersion:   BinaryVersion,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// validateAuth extracts and validates the Bearer token from the request.
// Returns the token string on success, or a *cliperr.Error on failure.
func (s *Server) validateAuth(r *http.Request) (string, *cliperr.Error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", cliperr.New("CB2002")
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", cliperr.New("CB2001")
	}

	provided := parts[1]

	expected, err := s.tokenStore.Retrieve()
	if err != nil {
		s.logger.Error("failed to retrieve stored token", "err", err)
		return "", cliperr.New("CB2001")
	}

	if !auth.ValidateToken(provided, expected) {
		return "", cliperr.New("CB2001")
	}

	return provided, nil
}

// extractRemoteIP returns the remote IP address from the request.
func extractRemoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
