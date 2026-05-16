package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Blob represents an encrypted clipboard payload.
type Blob struct {
	ID        string    `json:"id"`
	DeviceID  string    `json:"device_id"`
	Format    string    `json:"format"`
	Data      string    `json:"data"`      // base64 encrypted
	Nonce     string    `json:"nonce"`     // base64 nonce
	Timestamp time.Time `json:"timestamp"`
	TTL       int       `json:"ttl"`       // seconds
}

// Device represents a registered device.
type Device struct {
	DeviceID    string    `json:"device_id"`
	PublicKey   string    `json:"public_key"`
	Fingerprint string    `json:"fingerprint"`
	Token       string    `json:"token"`
	LastSeen    time.Time `json:"last_seen"`
}

// Peer represents a peer relationship.
type Peer struct {
	DeviceID    string `json:"device_id"`
	PeerDeviceID string `json:"peer_device_id"`
}

// Server is the relay server.
type Server struct {
	mu       sync.RWMutex
	blobs    map[string]*Blob       // blob ID -> blob
	devices  map[string]*Device     // device ID -> device
	peers    map[string][]string    // device ID -> peer device IDs
	tokens   map[string]string      // token -> device ID
	blobIdx  map[string]string      // device ID -> latest blob ID (legacy)
	inbox    map[string]map[string]string // receiver -> (sender -> blob ID)
}

func main() {
	port := "7332"
	if envPort := os.Getenv("PORT"); envPort != "" {
		port = envPort
	}

	s := &Server{
		blobs:   make(map[string]*Blob),
		devices: make(map[string]*Device),
		peers:   make(map[string][]string),
		tokens:  make(map[string]string),
		blobIdx: make(map[string]string),
		inbox:   make(map[string]map[string]string),
	}

	// Start cleanup goroutine.
	go s.cleanupExpiredBlobs()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/register", s.handleRegister)
	mux.HandleFunc("/api/v1/upload", s.handleUpload)      // legacy: upload to self
	mux.HandleFunc("/api/v1/upload/", s.handleUploadTo)   // new: /api/v1/upload/{receiver}
	mux.HandleFunc("/api/v1/download/", s.handleDownload) // requires auth
	mux.HandleFunc("/api/v1/devices", s.handleListDevices)
	mux.HandleFunc("/api/v1/peers", s.handlePeers) // GET list, POST add
	mux.HandleFunc("/health", s.handleHealth)

	addr := ":" + port
	log.Printf("relay-server listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
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
	if _, exists := s.devices[req.DeviceID]; exists {
		// Update last seen.
		s.devices[req.DeviceID].LastSeen = time.Now()
		token := s.devices[req.DeviceID].Token
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "token": token})
		return
	}

	// Generate token.
	token := generateToken()

	device := &Device{
		DeviceID:    req.DeviceID,
		PublicKey:   req.PublicKey,
		Fingerprint: req.Fingerprint,
		Token:       token,
		LastSeen:    time.Now(),
	}

	s.devices[req.DeviceID] = device
	s.tokens[token] = req.DeviceID

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
	deviceID, ok := s.tokens[token]
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
	blob := &Blob{
		ID:        blobID,
		DeviceID:  req.DeviceID,
		Format:    req.Format,
		Data:      req.Data,
		Nonce:     req.Nonce,
		Timestamp: time.Unix(req.Timestamp, 0),
		TTL:       req.TTL,
	}

	s.mu.Lock()
	s.blobs[blobID] = blob
	s.blobIdx[req.DeviceID] = blobID
	// Update last seen.
	if dev, exists := s.devices[req.DeviceID]; exists {
		dev.LastSeen = time.Now()
	}
	s.mu.Unlock()

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
	blobID, ok := s.blobIdx[deviceID]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "no data available", http.StatusNotFound)
		return
	}

	s.mu.RLock()
	blob, ok := s.blobs[blobID]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "blob not found", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"ok":       true,
		"blob_id":  blob.ID,
		"device_id": blob.DeviceID,
		"format":   blob.Format,
		"data":     blob.Data,
		"nonce":    blob.Nonce,
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
	_, ok := s.tokens[token]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	s.mu.RLock()
	var devices []map[string]any
	for _, dev := range s.devices {
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

func (s *Server) handleAddPeer(w http.ResponseWriter, r *http.Request) {
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
	token = token[7:]

	s.mu.RLock()
	deviceID, ok := s.tokens[token]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
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

	// Check if peer exists.
	s.mu.RLock()
	_, peerExists := s.devices[req.PeerDeviceID]
	s.mu.RUnlock()

	if !peerExists {
		http.Error(w, "peer device not found", http.StatusNotFound)
		return
	}

	s.mu.Lock()
	s.peers[deviceID] = append(s.peers[deviceID], req.PeerDeviceID)
	s.mu.Unlock()

	json.NewEncoder(w).Encode(map[string]any{"ok": true})
	log.Printf("peer added: %s -> %s", deviceID, req.PeerDeviceID)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	deviceCount := len(s.devices)
	blobCount := len(s.blobs)
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
		for id, blob := range s.blobs {
			if blob.TTL > 0 && now.Sub(blob.Timestamp) > time.Duration(blob.TTL)*time.Second {
				delete(s.blobs, id)
			}
		}
		s.mu.Unlock()
	}
}

func generateToken() string {
	b := make([]byte, 16)
	for i := range b {
		b[i] = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"[randByte()]
	}
	return string(b)
}

func generateBlobID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func randByte() byte {
	return byte(time.Now().UnixNano() % 62)
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

// New: upload to a specific receiver's inbox: /api/v1/upload/{receiver}
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
senderID, ok := s.tokens[token]
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
blob := &Blob{
ID:        blobID,
DeviceID:  req.DeviceID, // sender
Format:    req.Format,
Data:      req.Data,
Nonce:     req.Nonce,
Timestamp: time.Unix(req.Timestamp, 0),
TTL:       req.TTL,
}
s.mu.Lock()
s.blobs[blobID] = blob
if s.inbox[receiverID] == nil {
s.inbox[receiverID] = make(map[string]string)
}
s.inbox[receiverID][senderID] = blobID
if dev, exists := s.devices[senderID]; exists {
dev.LastSeen = time.Now()
}
s.mu.Unlock()
json.NewEncoder(w).Encode(map[string]any{"ok": true, "blob_id": blobID, "received": len(req.Data)})
}

// New: peers endpoint supports GET (list) and POST (add)
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
deviceID, ok := s.tokens[token]
s.mu.RUnlock()
if !ok {
http.Error(w, "unauthorized", http.StatusUnauthorized)
return
}
if r.Method == http.MethodGet {
s.mu.RLock()
var items []map[string]any
for _, pid := range s.peers[deviceID] {
if dev, ok := s.devices[pid]; ok {
items = append(items, map[string]any{
"device_id":  dev.DeviceID,
"public_key": dev.PublicKey,
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
_, peerExists := s.devices[req.PeerDeviceID]
s.mu.RUnlock()
if !peerExists {
http.Error(w, "peer device not found", http.StatusNotFound)
return
}
s.mu.Lock()
s.peers[deviceID] = append(s.peers[deviceID], req.PeerDeviceID)
s.mu.Unlock()
json.NewEncoder(w).Encode(map[string]any{"ok": true})
log.Printf("peer added: %s -> %s", deviceID, req.PeerDeviceID)
}
