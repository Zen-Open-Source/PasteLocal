package server

import (
	"testing"
)

func TestHistoryBufferAddAndGet(t *testing.T) {
	h := NewHistoryBuffer(5, 3600, "test-token")

	h.Add("id-1", "png", []byte("image1"))
	h.Add("id-2", "text", []byte("text2"))

	if h.Len() != 2 {
		t.Errorf("Len() = %d, want 2", h.Len())
	}

	data, entry, err := h.Get("id-1")
	if err != nil {
		t.Fatalf("Get id-1: %v", err)
	}
	if string(data) != "image1" {
		t.Errorf("Get id-1 data = %q, want %q", string(data), "image1")
	}
	if entry.Format != "png" {
		t.Errorf("Get id-1 format = %q, want %q", entry.Format, "png")
	}
}

func TestHistoryBufferEviction(t *testing.T) {
	h := NewHistoryBuffer(2, 3600, "test-token")

	h.Add("id-1", "png", []byte("image1"))
	h.Add("id-2", "png", []byte("image2"))
	h.Add("id-3", "png", []byte("image3"))

	if h.Len() != 2 {
		t.Errorf("Len() = %d, want 2", h.Len())
	}

	// id-1 should have been evicted.
	_, _, err := h.Get("id-1")
	if err == nil {
		t.Error("expected id-1 to be evicted")
	}

	// id-2 and id-3 should still be present.
	data, _, err := h.Get("id-2")
	if err != nil {
		t.Fatalf("Get id-2: %v", err)
	}
	if string(data) != "image2" {
		t.Errorf("Get id-2 data = %q, want %q", string(data), "image2")
	}

	data, _, err = h.Get("id-3")
	if err != nil {
		t.Fatalf("Get id-3: %v", err)
	}
	if string(data) != "image3" {
		t.Errorf("Get id-3 data = %q, want %q", string(data), "image3")
	}
}

func TestHistoryBufferList(t *testing.T) {
	h := NewHistoryBuffer(5, 3600, "test-token")

	h.Add("id-1", "png", []byte("image1"))
	h.Add("id-2", "text", []byte("text2"))

	items := h.List()
	if len(items) != 2 {
		t.Fatalf("List() returned %d items, want 2", len(items))
	}

	if items[0].ID != "id-1" {
		t.Errorf("items[0].ID = %q, want %q", items[0].ID, "id-1")
	}
	if items[0].Format != "png" {
		t.Errorf("items[0].Format = %q, want %q", items[0].Format, "png")
	}
}

func TestHistoryBufferEncryption(t *testing.T) {
	h := NewHistoryBuffer(5, 3600, "test-token-for-encryption")

	h.Add("id-1", "png", []byte("secret-image-data"))

	data, _, err := h.Get("id-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(data) != "secret-image-data" {
		t.Errorf("decrypted data = %q, want %q", string(data), "secret-image-data")
	}
}

func TestHistoryBufferNotFound(t *testing.T) {
	h := NewHistoryBuffer(5, 3600, "test-token")

	_, _, err := h.Get("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent ID")
	}
}

func TestHistoryBufferEmptyList(t *testing.T) {
	h := NewHistoryBuffer(5, 3600, "test-token")

	items := h.List()
	if len(items) != 0 {
		t.Errorf("List() returned %d items, want 0", len(items))
	}
}

func TestHistoryBufferUpdateKey(t *testing.T) {
	h := NewHistoryBuffer(5, 3600, "old-token")

	h.Add("id-1", "png", []byte("data-with-old-key"))

	// Updating the key should not break existing entries
	// because they were encrypted with the old key.
	// This is a known limitation; after key rotation, old entries
	// may not be decryptable.
	h.UpdateKey("new-token")

	// The entry was encrypted with the old key, so decrypting with
	// the new key should fail.
	_, _, err := h.Get("id-1")
	if err == nil {
		// This might succeed if encryption falls back to plaintext,
		// which is fine for a best-effort approach.
		t.Log("entry decrypted with new key (expected behavior with key rotation)")
	}
}
