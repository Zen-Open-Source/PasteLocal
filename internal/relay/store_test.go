package relay

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileStore_RoundtripAndCompact(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")

	st, err := NewStore(statePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// seed some data
	st.mu.Lock()
	st.Devices["dev1"] = &Device{DeviceID: "dev1", PublicKey: "pk1", Fingerprint: "fp1", Token: "tok1", LastSeen: time.Now()}
	st.Blobs["b1"] = &Blob{ID: "b1", DeviceID: "dev1", Format: "text", Data: "ZGF0YQ==", Nonce: "bm9uY2U=", Timestamp: time.Now(), TTL: 3600}
	st.Inbox["dev1"] = map[string]string{"sender1": "b1"}
	st.mu.Unlock()

	if err := st.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// new instance should load it
	st2, err := NewStore(statePath)
	if err != nil {
		t.Fatalf("NewStore reload: %v", err)
	}
	if len(st2.Devices) != 1 || st2.Devices["dev1"].Fingerprint != "fp1" {
		t.Errorf("reload mismatch: %+v", st2.Devices)
	}
	if len(st2.Blobs) != 1 {
		t.Errorf("blob not persisted")
	}

	// expire the blob and compact
	st2.mu.Lock()
	st2.Blobs["b1"].TTL = 1
	st2.Blobs["b1"].Timestamp = time.Now().Add(-2 * time.Hour)
	st2.mu.Unlock()

	if err := st2.Compact(); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if len(st2.Blobs) != 0 {
		t.Errorf("expired blob not removed by compact")
	}
	// also verify BlobIdx prune on reload after expire
	st3, err := NewStore(statePath)
	if err != nil {
		t.Fatalf("reload after Compact: %v", err)
	}
	if len(st3.BlobIdx) != 0 {
		t.Errorf("stale BlobIdx not pruned on reload")
	}
}

func TestStore_InMemoryMode(t *testing.T) {
	st, err := NewStore("")
	if err != nil {
		t.Fatal(err)
	}
	st.Devices["x"] = &Device{DeviceID: "x"}
	if err := st.Save(); err != nil {
		t.Error("mem save should be no-op")
	}
	if len(st.Devices) != 1 {
		t.Error("mem data lost")
	}
}
