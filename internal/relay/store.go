package relay

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// RelayStoreInterface is the persistence contract (v1: concrete only with
// exported fields for minimal diff; future SQLite etc. can implement).
type RelayStoreInterface interface {
	Load() error
	Save() error
	Compact() error
}

// RelayStore is a file-backed (or in-memory) store for relay server state.
// It uses a single state.json with atomic writes for durability.
// When path == "", operates in pure in-memory mode (no persistence).
type RelayStore struct {
	path string

	mu      sync.RWMutex
	Blobs   map[string]*Blob
	Devices map[string]*Device
	Peers   map[string][]string
	Inbox   map[string]map[string]string // receiver -> sender -> blobID
	Tokens  map[string]string            // token -> deviceID
	BlobIdx map[string]string            // deviceID -> latest legacy blobID
}

// PersistentState is the on-disk JSON shape.
type PersistentState struct {
	Blobs   map[string]*Blob            `json:"blobs"`
	Devices map[string]*Device          `json:"devices"`
	Peers   map[string][]string         `json:"peers"`
	Inbox   map[string]map[string]string `json:"inbox"`
	Tokens  map[string]string           `json:"tokens"`
	BlobIdx map[string]string           `json:"blob_idx"`
}

// NewStore creates a RelayStore. If statePath is non-empty, it loads from
// state.json in that dir (creating dir if needed). Otherwise pure in-mem.
func NewStore(statePath string) (*RelayStore, error) {
	s := &RelayStore{
		path:    statePath,
		Blobs:   make(map[string]*Blob),
		Devices: make(map[string]*Device),
		Peers:   make(map[string][]string),
		Inbox:   make(map[string]map[string]string),
		Tokens:  make(map[string]string),
		BlobIdx: make(map[string]string),
	}

	if statePath != "" {
		dir := filepath.Dir(statePath)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, fmt.Errorf("create state dir: %w", err)
		}
		if err := s.Load(); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("load initial state: %w", err)
		}
	}
	return s, nil
}

// Load reads state.json (if path set) and populates maps, dropping expired blobs.
func (s *RelayStore) Load() error {
	if s.path == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}

	var ps PersistentState
	if err := json.Unmarshal(data, &ps); err != nil {
		return fmt.Errorf("unmarshal state: %w", err)
	}

	now := time.Now()
	s.Blobs = make(map[string]*Blob)
	for id, b := range ps.Blobs {
		if b != nil && b.TTL > 0 && now.Sub(b.Timestamp) > time.Duration(b.TTL)*time.Second {
			continue // drop expired
		}
		if b != nil {
			s.Blobs[id] = b
		}
	}

	s.Devices = ps.Devices
	if s.Devices == nil {
		s.Devices = make(map[string]*Device)
	}
	s.Peers = ps.Peers
	if s.Peers == nil {
		s.Peers = make(map[string][]string)
	}
	s.Tokens = ps.Tokens
	if s.Tokens == nil {
		s.Tokens = make(map[string]string)
	}
	s.BlobIdx = ps.BlobIdx
	if s.BlobIdx == nil {
		s.BlobIdx = make(map[string]string)
	}

	// Rebuild inbox only with live blobs; also prune stale BlobIdx
	s.Inbox = make(map[string]map[string]string)
	for rcv, senders := range ps.Inbox {
		for snd, bid := range senders {
			if _, live := s.Blobs[bid]; live {
				if s.Inbox[rcv] == nil {
					s.Inbox[rcv] = make(map[string]string)
				}
				s.Inbox[rcv][snd] = bid
			}
		}
		if s.Inbox[rcv] != nil && len(s.Inbox[rcv]) == 0 {
			delete(s.Inbox, rcv)
		}
	}
	for did, bid := range s.BlobIdx {
		if _, live := s.Blobs[bid]; !live {
			delete(s.BlobIdx, did)
		}
	}
	return nil
}

// Save writes current state to disk atomically (if path set). Never fails in mem mode.
func (s *RelayStore) Save() error {
	if s.path == "" {
		return nil
	}
	s.mu.RLock()
	ps := PersistentState{
		Blobs:   s.Blobs,
		Devices: s.Devices,
		Peers:   s.Peers,
		Inbox:   s.Inbox,
		Tokens:  s.Tokens,
		BlobIdx: s.BlobIdx,
	}
	s.mu.RUnlock()

	data, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	return s.writeAtomic(data)
}

// writeAtomic performs temp+fsync+rename using same dir for atomicity + 0600 perms.
func (s *RelayStore) writeAtomic(data []byte) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("mkdir for atomic: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".relay-state-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()

	cleanup := func() {
		tmp.Close()
		os.Remove(tmpPath)
	}

	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Chmod(tmpPath, 0600); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp: %w", err)
	}
	return nil
}

// Compact drops expired blobs/inbox refs then Save(). Safe to call periodically.
func (s *RelayStore) Compact() error {
	s.mu.Lock()
	now := time.Now()
	var toDelete []string
	for id, b := range s.Blobs {
		if b.TTL > 0 && now.Sub(b.Timestamp) > time.Duration(b.TTL)*time.Second {
			toDelete = append(toDelete, id)
		}
	}
	for _, id := range toDelete {
		delete(s.Blobs, id)
		// clean indexes (collect first to avoid range-delete UB)
		var idxToDelete []string
		for did, bid := range s.BlobIdx {
			if bid == id {
				idxToDelete = append(idxToDelete, did)
			}
		}
		for _, did := range idxToDelete {
			delete(s.BlobIdx, did)
		}
		var rcvToPrune []string
		for rcv, senders := range s.Inbox {
			for snd, bid := range senders {
				if bid == id {
					delete(senders, snd)
				}
			}
			if len(senders) == 0 {
				rcvToPrune = append(rcvToPrune, rcv)
			}
		}
		for _, rcv := range rcvToPrune {
			delete(s.Inbox, rcv)
		}
	}
	s.mu.Unlock()
	// always Save after potential mutation (durability for expirations)
	if err := s.Save(); err != nil {
		// best-effort; caller (ticker) logs if needed
	}
	return nil
}

// Snapshot returns a copy of current maps (for tests or debug). Caller must not mutate.
func (s *RelayStore) Snapshot() (blobs map[string]*Blob, devices map[string]*Device, peers map[string][]string, inbox map[string]map[string]string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// shallow copy maps
	blobs = make(map[string]*Blob, len(s.Blobs))
	for k, v := range s.Blobs {
		blobs[k] = v
	}
	devices = make(map[string]*Device, len(s.Devices))
	for k, v := range s.Devices {
		devices[k] = v
	}
	peers = make(map[string][]string, len(s.Peers))
	for k, v := range s.Peers {
		cp := make([]string, len(v))
		copy(cp, v)
		peers[k] = cp
	}
	inbox = make(map[string]map[string]string, len(s.Inbox))
	for rcv, m := range s.Inbox {
		im := make(map[string]string, len(m))
		for k, v := range m {
			im[k] = v
		}
		inbox[rcv] = im
	}
	return
}
