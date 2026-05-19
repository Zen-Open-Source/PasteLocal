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

	"github.com/pastelocal/pastelocal/internal/auth"
	"github.com/pastelocal/pastelocal/internal/clipboard"
	"github.com/pastelocal/pastelocal/internal/crypto"
	cliperr "github.com/pastelocal/pastelocal/internal/errors"
	"github.com/pastelocal/pastelocal/internal/proto"
)

// handleClipboard handles GET /clipboard (read) and POST /clipboard (write).
func (s *Server) handleClipboard(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleClipboardGet(w, r)
	case http.MethodPost:
		s.handleClipboardPost(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleClipboardGet reads the current clipboard and returns it as JSON.
func (s *Server) handleClipboardGet(w http.ResponseWriter, r *http.Request) {
	// Step 1: Acquire semaphore (max_in_flight).
	select {
	case <-s.sem:
		defer func() { s.sem <- struct{}{} }()
	default:
		w.Header().Set("Retry-After", "1")
		cliperr.WriteJSON(w, cliperr.New("CB4002"))
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

	// Step 3b: Check read permission for identified host.
	if alias := s.identifyHost(token); alias != "" {
		if !s.cfg.HostHasPermission(alias, "read") {
			cliperr.WriteJSON(w, cliperr.New("CB2003"))
			return
		}
	}

	// Step 3c: Sensitive/concealed filter (password manager safety net).
	// Blocks explicit reads of items marked with ConcealedType etc.
	// Performed before acquiring the read lock and before any Read* call so
	// secret bytes are never materialized in the daemon for a filtered item.
	if s.cfg.Watch.Sensitive.FilterConcealed {
		concealed, cErr := s.reader.IsConcealed(r.Context())
		if cErr != nil {
			// Detector failure — log the full err (incl. stderr) for observability.
			// Fail-open (treat as non-concealed) per spec threat model; the log
			// ensures the (rare) risk of a missed filter is auditable.
			if s.cfg.Watch.Sensitive.LogFilteredItems {
				s.logger.Info("clipboard read: concealed detection failed (fail-open)", "err", cErr)
			} else {
				s.logger.Debug("clipboard read: concealed detection failed (fail-open)", "err", cErr)
			}
		} else if concealed {
			if s.cfg.Watch.Sensitive.LogFilteredItems {
				s.logger.Info("clipboard read: blocked concealed/sensitive item (ConcealedType)")
			}
			cliperr.WriteJSON(w, cliperr.New("CB1013"))
			return
		}
	}

	// Step 4: Read clipboard content with format detection.
	s.mu.Lock()
	content, readErr := s.reader.ReadContent(r.Context())
	if readErr != nil {
		s.mu.Unlock()
		if ce, ok := readErr.(*cliperr.Error); ok {
			cliperr.WriteJSON(w, ce)
			return
		}
		cliperr.WriteJSON(w, cliperr.NewWithMessage("CB1003", readErr.Error()))
		return
	}

	// Step 5: Enforce size limits.
	if content.Format == "png" && int64(len(content.Data)) > s.cfg.MaxImageBytes {
		s.mu.Unlock()
		cliperr.WriteJSON(w, cliperr.New("CB1005"))
		return
	}
	if content.Format == "text" && int64(len(content.Data)) > s.cfg.MaxTextBytes {
		s.mu.Unlock()
		cliperr.WriteJSON(w, cliperr.NewWithMessage("CB1005",
			"Text exceeds max_text_bytes"))
		return
	}

	// Step 5b: Check format is allowed.
	if !s.cfg.IsFormatAllowed(content.Format) {
		s.mu.Unlock()
		cliperr.WriteJSON(w, cliperr.New("CB1008"))
		return
	}

	// Step 5c: Run redaction checks.
	if content.Format == "text" {
		textContent := string(content.Data)
		if redactErr := s.redaction.CheckText(&textContent); redactErr != nil {
			s.mu.Unlock()
			cliperr.WriteJSON(w, redactErr)
			return
		}
		content.Data = []byte(textContent)
	} else if content.Format == "png" {
		if redactErr := s.redaction.CheckImageData(content.Data); redactErr != nil {
			s.mu.Unlock()
			cliperr.WriteJSON(w, redactErr)
			return
		}
	}

	// Step 5d: Run processor pipeline on read.
	if procErr := s.processors.ProcessRead(r.Context(), content); procErr != nil {
		s.mu.Unlock()
		cliperr.WriteJSON(w, procErr)
		return
	}

	now := time.Now().UTC()
	s.lastRead = now
	s.lastReadSize = int64(len(content.Data))
	s.lastReadFormat = content.Format
	s.mu.Unlock()

	// Step 5e: Run vision analysis pipeline (post-processors, post-unlock so we do not
	// hold the read mutex during potentially slow external commands like tesseract).
	// Only images; concealed items never reach here. Fail-open inside Analyze.
	var analysis *AnalysisResult
	analysis = s.analysis.Analyze(r.Context(), content)

	// Step 6: Build response based on format.
	resp := proto.ClipboardResponse{
		OK:         true,
		Format:     content.Format,
		ByteCount:  int64(len(content.Data)),
		CapturedAt: now.Format(time.RFC3339),
	}

	if content.Format == "png" {
		resp.Image = base64.StdEncoding.EncodeToString(content.Data)
	} else {
		resp.Text = string(content.Data)
	}

	if analysis != nil {
		resp.Analysis = &proto.ClipboardAnalysis{
			OCRText:     analysis.OCRText,
			Description: analysis.Description,
		}
	}

	// Step 7: Add to history if enabled.
	entryID := ""
	if s.history != nil {
		entryID = generateEntryID(now, content.Format)
		resp.ID = entryID
		s.history.Add(entryID, content.Format, content.Data)
	}

	// Step 8: Notify watchers.
	s.watchHub.Notify(proto.WatchNotification{
		Event:      "clipboard_changed",
		CapturedAt: now.Format(time.RFC3339),
		Format:     content.Format,
		ID:         entryID,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)

	// Step 9: Audit logging if configured.
	if s.cfg.AuditLog != "" {
		hash := sha256.Sum256(content.Data)
		entry := AuditEntry{
			Event:     "clipboard_read",
			ImageHash: fmt.Sprintf("%x", hash),
			ByteCount: int64(len(content.Data)),
			SourceIP:  remoteIP,
			Auth:      token,
		}
		if err := WriteAudit(s.cfg.AuditLog, entry); err != nil {
			s.logger.Error("audit log write failed", "err", err)
		}
	}
}

// handleClipboardPost writes content to the local clipboard.
func (s *Server) handleClipboardPost(w http.ResponseWriter, r *http.Request) {
	// Step 1: Acquire semaphore.
	select {
	case <-s.sem:
		defer func() { s.sem <- struct{}{} }()
	default:
		w.Header().Set("Retry-After", "1")
		cliperr.WriteJSON(w, cliperr.New("CB4002"))
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
		s.logger.Info("auth failed on write",
			"auth_failed", true,
			"code", authErr.Code,
			"remote_ip", remoteIP,
		)
		cliperr.WriteJSON(w, authErr)
		return
	}

	// Step 3b: Check write permission.
	if alias := s.identifyHost(token); alias != "" {
		if !s.cfg.HostHasPermission(alias, "write") {
			cliperr.WriteJSON(w, cliperr.New("CB2003"))
			return
		}
	}

	// Step 4: Decode request body.
	var req proto.ClipboardWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		cliperr.WriteJSON(w, cliperr.NewWithMessage("CB1008",
			"invalid JSON in request body"))
		return
	}

	// Step 5: Validate format.
	if req.Format != "png" && req.Format != "text" {
		cliperr.WriteJSON(w, cliperr.New("CB1007"))
		return
	}
	if !s.cfg.IsFormatAllowed(req.Format) {
		cliperr.WriteJSON(w, cliperr.New("CB1008"))
		return
	}

	// Step 6: Build content from request.
	content := &clipboard.Content{Format: req.Format}
	switch req.Format {
	case "png":
		if req.Image == "" {
			cliperr.WriteJSON(w, cliperr.NewWithMessage("CB1008",
				"missing image field for png format"))
			return
		}
		data, err := base64.StdEncoding.DecodeString(req.Image)
		if err != nil {
			cliperr.WriteJSON(w, cliperr.NewWithMessage("CB1004",
				"base64 decode failed: "+err.Error()))
			return
		}
		if int64(len(data)) > s.cfg.MaxImageBytes {
			cliperr.WriteJSON(w, cliperr.New("CB1005"))
			return
		}
		// Redaction check on image data.
		if redactErr := s.redaction.CheckImageData(data); redactErr != nil {
			cliperr.WriteJSON(w, redactErr)
			return
		}
		content.Data = data
	case "text":
		if req.Text == "" {
			cliperr.WriteJSON(w, cliperr.NewWithMessage("CB1008",
				"missing text field for text format"))
			return
		}
		if int64(len(req.Text)) > s.cfg.MaxTextBytes {
			cliperr.WriteJSON(w, cliperr.NewWithMessage("CB1005",
				"Text exceeds max_text_bytes"))
			return
		}
		// Redaction check on text.
		textContent := req.Text
		if redactErr := s.redaction.CheckText(&textContent); redactErr != nil {
			cliperr.WriteJSON(w, redactErr)
			return
		}
		content.Data = []byte(textContent)
	}

	// Step 7: Run processor pipeline on write.
	if procErr := s.processors.ProcessWrite(r.Context(), content); procErr != nil {
		cliperr.WriteJSON(w, procErr)
		return
	}

	// Step 8: Write to clipboard.
	if err := s.writer.Write(r.Context(), content); err != nil {
		cliperr.WriteJSON(w, cliperr.NewWithMessage("CB1006", err.Error()))
		return
	}

	// Step 8b: Upload to relay peers if enabled and auto-upload is on (non-blocking, best-effort).
	if s.relayClient != nil && s.cfg.Relay.AutoUpload {
		go s.pushToRelayPeers(content.Format, content.Data)
	}

	// Step 9: Build success response.
	resp := proto.ClipboardWriteResponse{
		OK:        true,
		Format:    req.Format,
		ByteCount: int64(len(content.Data)),
	}

	// Step 10: Audit log if configured.
	if s.cfg.AuditLog != "" {
		hash := sha256.Sum256(content.Data)
		entry := AuditEntry{
			Event:     "clipboard_write",
			ImageHash: fmt.Sprintf("%x", hash),
			ByteCount: int64(len(content.Data)),
			SourceIP:  remoteIP,
			Auth:      token,
		}
		if err := WriteAudit(s.cfg.AuditLog, entry); err != nil {
			s.logger.Error("audit log write failed", "err", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleClipboardHistory handles GET /clipboard/history and /clipboard/history/{id}.
func (s *Server) handleClipboardHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Auth check.
	if _, authErr := s.validateAuth(r); authErr != nil {
		cliperr.WriteJSON(w, authErr)
		return
	}

	// Check if requesting a specific entry by ID (path: /clipboard/history/{id})
	path := strings.TrimPrefix(r.URL.Path, "/clipboard/history")
	path = strings.TrimPrefix(path, "/")

	if s.history == nil {
		if path != "" {
			// Specific entry requested but history disabled.
			cliperr.WriteJSON(w, cliperr.NewWithMessage("CB1009", "history is disabled"))
			return
		}
		// List requested but history disabled.
		resp := proto.HistoryResponse{OK: true, Items: []proto.HistoryEntry{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	// Fetch specific entry by ID.
	if path != "" {
		entryID := path
		data, entry, err := s.history.Get(entryID)
		if err != nil {
			cliperr.WriteJSON(w, cliperr.NewWithMessage("CB1009", "history entry not found or expired"))
			return
		}

		// Build response similar to clipboard read.
		resp := proto.ClipboardResponse{
			OK:         true,
			Format:     entry.Format,
			ByteCount:  entry.ByteCount,
			CapturedAt: entry.CapturedAt.Format(time.RFC3339),
			ID:         entry.ID,
		}

		if entry.Format == "png" {
			resp.Image = base64.StdEncoding.EncodeToString(data)
		} else {
			resp.Text = string(data)
		}

		// VisionPaste: demand-driven re-analysis on history fetch (explicit read of
		// historical bytes). Keeps history storage unchanged (raw only) while still
		// delivering rich context + sidecar for agents using --list/--index.
		// Only runs if vision enabled; fail-open.
		//
		// Note: intentionally operates on stored historical bytes and therefore
		// skips the live IsConcealed + redaction + processor gates that protect
		// the primary /clipboard read path (those checks are impossible on past data).
		// Historical entries were already vetted at original capture time.
		if entry.Format == "png" {
			tmp := &clipboard.Content{Data: data, Format: entry.Format}
			if ar := s.analysis.Analyze(r.Context(), tmp); ar != nil {
				resp.Analysis = &proto.ClipboardAnalysis{
					OCRText:     ar.OCRText,
					Description: ar.Description,
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	// List all history entries.
	items := s.history.List()
	if items == nil {
		items = []proto.HistoryEntry{}
	}

	resp := proto.HistoryResponse{OK: true, Items: items}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
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
	// Populate optional watch status (available even without auth, for local TUI/dashboard).
	if enabled, t := s.WatchStatus(); enabled || !t.IsZero() {
		resp.WatchEnabled = enabled
		if enabled && !t.IsZero() {
			resp.LastClipboardChange = t.Format(time.RFC3339)
		}
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

// identifyHost tries to find which host config matches the provided token.
// Returns empty string if no match found (could be using the global token).
func (s *Server) identifyHost(token string) string {
	// For now, return empty string. Per-host token matching will be
	// implemented when separate tokens are fully wired.
	return ""
}

// extractRemoteIP returns the remote IP address from the request.
func extractRemoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// generateEntryID creates a unique ID for a history entry.
func generateEntryID(t time.Time, format string) string {
	return fmt.Sprintf("%d-%s", t.UnixNano(), format)
}

// pushToRelayPeers encrypts the clipboard content for each configured peer
// and uploads to their relay inbox. Called in goroutine from write path and watcher.
// Never blocks; logs warnings on failure.
func (s *Server) pushToRelayPeers(format string, data []byte) {
	if s.relayClient == nil || s.relayKeyPair == nil {
		return
	}
	peersResp, err := s.relayClient.ListPeers()
	if err != nil {
		s.logger.Warn("relay push: list peers failed", "err", err)
		return
	}
	if !peersResp.OK || len(peersResp.Peers) == 0 {
		return
	}

	ttl := s.cfg.Relay.UploadTTL
	if ttl <= 0 {
		ttl = 300
	}

	success := 0
	for _, p := range peersResp.Peers {
		pub, err := crypto.ParsePublicKey(p.PublicKey)
		if err != nil {
			s.logger.Warn("relay push: bad peer pubkey", "peer", p.DeviceID, "err", err)
			continue
		}
		if _, err := s.relayClient.EncryptAndUploadTo(pub, p.DeviceID, format, data, ttl); err != nil {
			s.logger.Warn("relay push: encrypt/upload failed for peer", "peer", p.Fingerprint, "err", err)
			continue
		}
		success++
	}
	if success > 0 {
		s.logger.Info("relay push: uploaded to peers", "peers", success, "format", format, "bytes", len(data))
	}
}
