package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// AuditEntry represents a single audit log entry.
type AuditEntry struct {
	Time      string `json:"time"`
	Event     string `json:"event"`
	ImageHash string `json:"image_hash"`
	ByteCount int64  `json:"byte_count"`
	SourceIP  string `json:"source_ip"`
	Auth      string `json:"auth"`
}

// WriteAudit appends a JSON-line audit entry to the file at path.
// It creates the parent directory if it does not exist.
// Each entry is written as a single JSON line terminated by a newline.
func WriteAudit(path string, entry AuditEntry) error {
	if path == "" {
		return nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	entry.Time = time.Now().UTC().Format(time.RFC3339)

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	data = append(data, '\n')
	_, err = f.Write(data)
	return err
}
