package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pastelocal/pastelocal/internal/relay"
)

// Server is the relay server. All data is delegated to the RelayStore
// (which may be file-backed for persistence or in-memory).
type Server struct {
	mu    sync.RWMutex
	store *relay.RelayStore
}

func main() {
	port := flag.String("port", "7332", "listen port")
	stateDir := flag.String("state-dir", "", "persist state dir (writes state.json); empty = in-memory only")
	flag.Parse()

	if envPort := os.Getenv("PORT"); envPort != "" {
		*port = envPort
	}

	persistPath := ""
	if *stateDir != "" {
		persistPath = filepath.Join(expandHome(*stateDir), "state.json")
	}
	st, err := relay.NewStore(persistPath)
	if err != nil {
		log.Fatalf("failed to init relay store: %v", err)
	}

	s := &Server{
		store: st,
	}

	// Start cleanup goroutine (also compacts persisted state).
	go s.cleanupExpiredBlobs()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/register", s.handleRegister)
	mux.HandleFunc("/api/v1/upload", s.handleUpload)      // legacy: upload to self
	mux.HandleFunc("/api/v1/upload/", s.handleUploadTo)   // new: /api/v1/upload/{receiver}
	mux.HandleFunc("/api/v1/download/", s.handleDownload) // requires auth
	mux.HandleFunc("/api/v1/devices", s.handleListDevices)
	mux.HandleFunc("/api/v1/peers", s.handlePeers) // GET list, POST add
	mux.HandleFunc("/api/v1/inbox", s.handleInboxList)
	mux.HandleFunc("/api/v1/inbox/", s.handleInboxFetch) // /api/v1/inbox/{sender}
	mux.HandleFunc("/health", s.handleHealth)

	addr := ":" + *port
	log.Printf("relay-server listening on %s (persist: %s)", addr, persistPath)
	if persistPath != "" {
		log.Printf("  state file: %s", persistPath)
	}
	log.Fatal(http.ListenAndServe(addr, mux))
}

// expandHome replaces a leading ~ with the user's home directory (for state-dir).
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

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		DeviceID    string `json:"device_id"`
		PublicKey   string `json:"public_key"`
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if req.DeviceID == "" || req.PublicKey == "" {
		http.Error(w, "missing device_id or public_key", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if device already exists.
	if _, exists := s.store.Devices[req.DeviceID]; exists {
		// Update last seen.
		s.store.Devices[req.DeviceID].LastSeen = time.Now()
		token := s.store.Devices[req.DeviceID].Token
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "token": token})
		if err := s.store.Save(); err != nil {
			log.Printf("relay persist save: %v", err)
		}
		return
	}

	// Generate token.
	token := generateToken()

	device := &relay.Device{
		DeviceID:    req.DeviceID,
		PublicKey:   req.PublicKey,
		Fingerprint: req.Fingerprint,
		Token:       token,
		LastSeen:    time.Now(),
	}

	s.store.Devices[req.DeviceID] = device
	s.store.Tokens[token] = req.DeviceID

	if err := s.store.Save(); err != nil {
		log.Printf("relay persist save: %v", err)
	}
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "token": token})
	log.Printf("device registered: %s (%s)", req.DeviceID, req.Fingerprint)
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Auth check.
	token := r.Header.Get("Authorization")
	if token == "" || len(token) < 7 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	token = token[7:] // Remove "Bearer "

	s.mu.RLock()
	deviceID, ok := s.store.Tokens[token]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		DeviceID  string `json:"device_id"`
		Format    string `json:"format"`
		Data      string `json:"data"`
		Nonce     string `json:"nonce"`
		Timestamp int64  `json:"timestamp"`
		TTL       int    `json:"ttl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if req.DeviceID != deviceID {
		http.Error(w, "device_id mismatch", http.StatusUnauthorized)
		return
	}

	blobID := generateBlobID()
	blob := &relay.Blob{
		ID:        blobID,
		DeviceID:  req.DeviceID,
		Format:    req.Format,
		Data:      req.Data,
		Nonce:     req.Nonce,
		Timestamp: time.Unix(req.Timestamp, 0),
		TTL:       req.TTL,
	}

	s.mu.Lock()
	s.store.Blobs[blobID] = blob
	s.store.BlobIdx[req.DeviceID] = blobID
	// Update last seen.
	if dev, exists := s.store.Devices[req.DeviceID]; exists {
		dev.LastSeen = time.Now()
	}
	s.mu.Unlock()
	if err := s.store.Save(); err != nil {
		log.Printf("relay persist save: %v", err)
	}

	json.NewEncoder(w).Encode(map[string]any{
		"ok":       true,
		"blob_id":  blobID,
		"received": len(req.Data),
	})
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract device ID from path: /api/v1/download/{device_id}
	deviceID := r.URL.Path[len("/api/v1/download/"):]
	if deviceID == "" {
		http.Error(w, "missing device_id", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	blobID, ok := s.store.BlobIdx[deviceID]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "no data available", http.StatusNotFound)
		return
	}

	s.mu.RLock()
	blob, ok := s.store.Blobs[blobID]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "blob not found", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"ok":        true,
		"blob_id":   blob.ID,
		"device_id": blob.DeviceID,
		"format":    blob.Format,
		"data":      blob.Data,
		"nonce":     blob.Nonce,
	})
}

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Auth check.
	token := r.Header.Get("Authorization")
	if token == "" || len(token) < 7 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	token = token[7:]

	s.mu.RLock()
	_, ok := s.store.Tokens[token]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	s.mu.RLock()
	var devices []map[string]any
	for _, dev := range s.store.Devices {
		devices = append(devices, map[string]any{
			"device_id":    dev.DeviceID,
			"public_key":   dev.PublicKey,
			"fingerprint":  dev.Fingerprint,
			"last_seen":    dev.LastSeen.Unix(),
		})
	}
	s.mu.RUnlock()

	json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"devices": devices,
	})
}

// handleInboxList returns pending clips in the authenticated device's inbox.
func (s *Server) handleInboxList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := parseBearer(r.Header.Get("Authorization"))
	if token == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	s.mu.RLock()
	deviceID, ok := s.store.Tokens[token]
	s.mu.RUnlock()
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	s.mu.RLock()
	senders := s.store.Inbox[deviceID]
	var pending []map[string]any
	for senderID, blobID := range senders {
		if blob, exists := s.store.Blobs[blobID]; exists {
			fp := ""
			if dev, ok := s.store.Devices[senderID]; ok {
				fp = dev.Fingerprint
			}
			pending = append(pending, map[string]any{
				"sender_device_id": senderID,
				"fingerprint":      fp,
				"format":           blob.Format,
				"timestamp":        blob.Timestamp.Unix(),
				"blob_id":          blobID,
			})
		}
	}
	s.mu.RUnlock()

	json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"pending": pending,
	})
}

// handleInboxFetch returns the encrypted blob for a specific sender in the inbox.
func (s *Server) handleInboxFetch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := parseBearer(r.Header.Get("Authorization"))
	if token == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	s.mu.RLock()
	deviceID, ok := s.store.Tokens[token]
	s.mu.RUnlock()
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	senderID := strings.TrimPrefix(r.URL.Path, "/api/v1/inbox/")
	if senderID == "" {
		http.Error(w, "missing sender_device_id", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	blobID, hasSender := s.store.Inbox[deviceID][senderID]
	blob, hasBlob := s.store.Blobs[blobID]
	s.mu.RUnlock()

	if !hasSender || !hasBlob {
		http.Error(w, "no data from that sender", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"ok":        true,
		"blob_id":   blob.ID,
		"device_id": blob.DeviceID, // the sender
		"format":    blob.Format,
		"data":      blob.Data,
		"nonce":     blob.Nonce,
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	deviceCount := len(s.store.Devices)
	blobCount := len(s.store.Blobs)
	s.mu.RUnlock()

	json.NewEncoder(w).Encode(map[string]any{
		"ok":          true,
		"devices":     deviceCount,
		"blobs":       blobCount,
		"version":     "1.0.0",
	})
}

func (s *Server) cleanupExpiredBlobs() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		var toExpire []string
		for id, blob := range s.store.Blobs {
			if blob.TTL > 0 && now.Sub(blob.Timestamp) > time.Duration(blob.TTL)*time.Second {
				toExpire = append(toExpire, id)
			}
		}
		for _, id := range toExpire {
			delete(s.store.Blobs, id)
			// Also remove from any inboxes (collect to avoid range-delete)
			var rcvToPrune []string
			for rcv, senders := range s.store.Inbox {
				for sender, bID := range senders {
					if bID == id {
						delete(senders, sender)
					}
				}
				if len(senders) == 0 {
					rcvToPrune = append(rcvToPrune, rcv)
				}
			}
			for _, rcv := range rcvToPrune {
				delete(s.store.Inbox, rcv)
			}
			var idxToDelete []string
			for did, bid := range s.store.BlobIdx {
				if bid == id {
					idxToDelete = append(idxToDelete, did)
				}
			}
			for _, did := range idxToDelete {
				delete(s.store.BlobIdx, did)
			}
		}
		s.mu.Unlock()
		if err := s.store.Compact(); err != nil {
			log.Printf("relay cleanup save: %v", err)
		}
	}
}

func generateToken() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// fallback (should never happen)
		for i := range b {
			b[i] = byte(time.Now().UnixNano() % 62)
		}
	} else {
		for i := range b {
			b[i] = alphabet[b[i]%byte(len(alphabet))]
		}
	}
	return string(b)
}

func generateBlobID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// keep imports used until full implementation lands
var (
_ = base64.RawURLEncoding
_ = rand.Reader
)

func parseBearer(h string) string {
	if h == "" {
		return ""
	}
	h = strings.TrimSpace(h)
	if len(h) < 7 || strings.ToLower(h[:7]) != "bearer " {
		return ""
	}
	return strings.TrimSpace(h[7:])
}

// handleUploadTo uploads an encrypted blob into a specific receiver's inbox.
func (s *Server) handleUploadTo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := parseBearer(r.Header.Get("Authorization"))
	if token == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	s.mu.RLock()
	senderID, ok := s.store.Tokens[token]
	s.mu.RUnlock()
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	receiverID := strings.TrimPrefix(r.URL.Path, "/api/v1/upload/")
	if receiverID == "" {
		http.Error(w, "missing receiver_device_id", http.StatusBadRequest)
		return
	}
	var req struct {
		DeviceID  string `json:"device_id"`
		Format    string `json:"format"`
		Data      string `json:"data"`
		Nonce     string `json:"nonce"`
		Timestamp int64  `json:"timestamp"`
		TTL       int    `json:"ttl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.DeviceID != senderID {
		http.Error(w, "device_id mismatch", http.StatusUnauthorized)
		return
	}
	blobID := generateBlobID()
	blob := &relay.Blob{
		ID:        blobID,
		DeviceID:  req.DeviceID, // sender
		Format:    req.Format,
		Data:      req.Data,
		Nonce:     req.Nonce,
		Timestamp: time.Unix(req.Timestamp, 0),
		TTL:       req.TTL,
	}
	s.mu.Lock()
	s.store.Blobs[blobID] = blob
	if s.store.Inbox[receiverID] == nil {
		s.store.Inbox[receiverID] = make(map[string]string)
	}
	s.store.Inbox[receiverID][senderID] = blobID
	if dev, exists := s.store.Devices[senderID]; exists {
		dev.LastSeen = time.Now()
	}
	s.mu.Unlock()
	if err := s.store.Save(); err != nil {
		log.Printf("relay persist save: %v", err)
	}
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "blob_id": blobID, "received": len(req.Data)})
}

// handlePeers supports GET (list my peers with pubkeys) and POST (add peer).
func (s *Server) handlePeers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := parseBearer(r.Header.Get("Authorization"))
	if token == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	s.mu.RLock()
	deviceID, ok := s.store.Tokens[token]
	s.mu.RUnlock()
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodGet {
		s.mu.RLock()
		var items []map[string]any
		for _, pid := range s.store.Peers[deviceID] {
			if dev, ok := s.store.Devices[pid]; ok {
				items = append(items, map[string]any{
					"device_id":   dev.DeviceID,
					"public_key":  dev.PublicKey,
					"fingerprint": dev.Fingerprint,
				})
			}
		}
		s.mu.RUnlock()
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "peers": items})
		return
	}
	var req struct {
		DeviceID     string `json:"device_id"`
		PeerDeviceID string `json:"peer_device_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.DeviceID != deviceID {
		http.Error(w, "device_id mismatch", http.StatusUnauthorized)
		return
	}
	s.mu.RLock()
	_, peerExists := s.store.Devices[req.PeerDeviceID]
	s.mu.RUnlock()
	if !peerExists {
		http.Error(w, "peer device not found", http.StatusNotFound)
		return
	}
	s.mu.Lock()
	s.store.Peers[deviceID] = append(s.store.Peers[deviceID], req.PeerDeviceID)
	s.mu.Unlock()
	if err := s.store.Save(); err != nil {
		log.Printf("relay persist save: %v", err)
	}
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
	log.Printf("peer added: %s -> %s", deviceID, req.PeerDeviceID)
}
