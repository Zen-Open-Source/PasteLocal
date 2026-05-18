package relay

import "time"

// Blob represents an encrypted clipboard payload persisted by the relay server.
type Blob struct {
	ID        string    `json:"id"`
	DeviceID  string    `json:"device_id"`
	Format    string    `json:"format"`
	Data      string    `json:"data"` // base64 encrypted
	Nonce     string    `json:"nonce"` // base64 nonce
	Timestamp time.Time `json:"timestamp"`
	TTL       int       `json:"ttl"` // seconds
}

// Device represents a registered device.
type Device struct {
	DeviceID    string    `json:"device_id"`
	PublicKey   string    `json:"public_key"`
	Fingerprint string    `json:"fingerprint"`
	Token       string    `json:"token"`
	LastSeen    time.Time `json:"last_seen"`
}

// Peer represents a peer relationship (deviceID -> list of peerDeviceIDs).
// Stored as map in state for simplicity.
type Peer struct {
	DeviceID     string `json:"device_id"`
	PeerDeviceID string `json:"peer_device_id"`
}
