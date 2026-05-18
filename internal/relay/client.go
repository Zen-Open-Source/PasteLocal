package relay

import (
	"bytes"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pastelocal/pastelocal/internal/crypto"
)

// Client is the relay client for uploading/downloading encrypted clipboard data.
type Client struct {
	relayURL  string
	deviceID  crypto.DeviceID
	keyPair   *crypto.KeyPair
	client    *http.Client
	authToken string
}

// NewClient creates a new relay client.
func NewClient(relayURL string, deviceID crypto.DeviceID, keyPair *crypto.KeyPair, authToken string) *Client {
	return &Client{
		relayURL:  relayURL,
		deviceID:  deviceID,
		keyPair:   keyPair,
		client:    &http.Client{Timeout: 30 * time.Second},
		authToken: authToken,
	}
}

// RegisterRequest is the JSON payload for device registration.
type RegisterRequest struct {
	DeviceID    string `json:"device_id"`
	PublicKey   string `json:"public_key"`
	Fingerprint string `json:"fingerprint"`
}

// RegisterResponse is the JSON response from registration.
type RegisterResponse struct {
	OK    bool   `json:"ok"`
	Token string `json:"token,omitempty"`
	Error string `json:"error,omitempty"`
}

// Register registers the device with the relay server.
func (c *Client) Register() (*RegisterResponse, error) {
	req := RegisterRequest{
		DeviceID:    string(c.deviceID),
		PublicKey:   c.keyPair.PublicKeyBase64(),
		Fingerprint: c.keyPair.Fingerprint(),
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	resp, err := c.client.Post(c.relayURL+"/api/v1/register", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("registering device: %w", err)
	}
	defer resp.Body.Close()

	var registerResp RegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&registerResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if !registerResp.OK {
		return &registerResp, fmt.Errorf("registration failed: %s", registerResp.Error)
	}

	c.authToken = registerResp.Token
	return &registerResp, nil
}

// UploadPayload is the encrypted payload to upload.
type UploadPayload struct {
	DeviceID    string `json:"device_id"`
	Format      string `json:"format"`      // "text" or "png"
	Data        string `json:"data"`        // base64 encrypted data
	Nonce       string `json:"nonce"`       // base64 nonce
	Timestamp   int64  `json:"timestamp"`   // unix timestamp
	TTL         int     `json:"ttl"`         // time-to-live in seconds
}

// UploadResponse is the response from upload.
type UploadResponse struct {
	OK       bool   `json:"ok"`
	BlobID   string `json:"blob_id,omitempty"`
	Error    string `json:"error,omitempty"`
	Received int64  `json:"received,omitempty"`
}

// Upload uploads encrypted clipboard data to the relay.
func (c *Client) Upload(format string, encryptedData []byte, nonce []byte, ttl int) (*UploadResponse, error) {
	payload := UploadPayload{
		DeviceID:  string(c.deviceID),
		Format:    format,
		Data:      base64.StdEncoding.EncodeToString(encryptedData),
		Nonce:     base64.StdEncoding.EncodeToString(nonce),
		Timestamp: time.Now().Unix(),
		TTL:       ttl,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling payload: %w", err)
	}

	req, err := http.NewRequest("POST", c.relayURL+"/api/v1/upload", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("uploading to relay: %w", err)
	}
	defer resp.Body.Close()

	var uploadResp UploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&uploadResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if !uploadResp.OK {
		return &uploadResp, fmt.Errorf("upload failed: %s", uploadResp.Error)
	}

	return &uploadResp, nil
}

// DownloadResponse is the response from download.
type DownloadResponse struct {
	OK       bool   `json:"ok"`
	BlobID   string `json:"blob_id,omitempty"`
	DeviceID string `json:"device_id,omitempty"`
	Format   string `json:"format,omitempty"`
	Data     string `json:"data,omitempty"` // base64 encrypted data
	Nonce    string `json:"nonce,omitempty"` // base64 nonce
	Error    string `json:"error,omitempty"`
}

// Download downloads the latest clipboard data from the relay.
func (c *Client) Download() (*DownloadResponse, error) {
	req, err := http.NewRequest("GET", c.relayURL+"/api/v1/download/"+string(c.deviceID), nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading from relay: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return &DownloadResponse{OK: false, Error: "no data available"}, nil
	}

	var downloadResp DownloadResponse
	if err := json.NewDecoder(resp.Body).Decode(&downloadResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &downloadResp, nil
}

// ListDevicesResponse is the response from listing devices.
type ListDevicesResponse struct {
	OK      bool                `json:"ok"`
	Devices []DeviceInfo        `json:"devices,omitempty"`
	Error   string              `json:"error,omitempty"`
}

// DeviceInfo represents a paired device.
type DeviceInfo struct {
	DeviceID    string `json:"device_id"`
	PublicKey   string `json:"public_key"`
	Fingerprint string `json:"fingerprint"`
	LastSeen    int64  `json:"last_seen"`
}

// ListDevices lists all devices registered on the relay.
func (c *Client) ListDevices() (*ListDevicesResponse, error) {
	req, err := http.NewRequest("GET", c.relayURL+"/api/v1/devices", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing devices: %w", err)
	}
	defer resp.Body.Close()

	var listResp ListDevicesResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &listResp, nil
}

// AddPeerRequest adds a peer device for encrypted sharing.
type AddPeerRequest struct {
	DeviceID    string `json:"device_id"`
	PeerDeviceID string `json:"peer_device_id"`
}

// AddPeerResponse is the response from adding a peer.
type AddPeerResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// AddPeer adds a peer device to enable encrypted sharing.
func (c *Client) AddPeer(peerDeviceID string) (*AddPeerResponse, error) {
	req := AddPeerRequest{
		DeviceID:     string(c.deviceID),
		PeerDeviceID: peerDeviceID,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", c.relayURL+"/api/v1/peers", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	if c.authToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.authToken)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("adding peer: %w", err)
	}
	defer resp.Body.Close()

	var addResp AddPeerResponse
	if err := json.NewDecoder(resp.Body).Decode(&addResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &addResp, nil
}

// EncryptAndUpload encrypts data for a peer and uploads to relay.
func (c *Client) EncryptAndUpload(peerPublicKey *ecdh.PublicKey, format string, data []byte, ttl int) (*UploadResponse, error) {
	// Compute shared secret.
	sharedSecret, err := c.keyPair.SharedSecret(peerPublicKey)
	if err != nil {
		return nil, fmt.Errorf("computing shared secret: %w", err)
	}

	// Generate nonce.
	nonce, err := crypto.GenerateNonce()
	if err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}

	// Encrypt data.
	encryptedData, err := crypto.Encrypt(data, sharedSecret, nonce)
	if err != nil {
		return nil, fmt.Errorf("encrypting data: %w", err)
	}

	// Upload.
	return c.Upload(format, encryptedData, nonce, ttl)
}

// DownloadAndDecrypt downloads data from relay and decrypts it.
func (c *Client) DownloadAndDecrypt(senderPublicKey *ecdh.PublicKey) ([]byte, string, error) {
	// Download.
	downloadResp, err := c.Download()
	if err != nil {
		return nil, "", err
	}

	if !downloadResp.OK {
		return nil, "", fmt.Errorf("download failed: %s", downloadResp.Error)
	}

	// Decode encrypted data and nonce.
	encryptedData, err := base64.StdEncoding.DecodeString(downloadResp.Data)
	if err != nil {
		return nil, "", fmt.Errorf("decoding data: %w", err)
	}

	nonce, err := base64.StdEncoding.DecodeString(downloadResp.Nonce)
	if err != nil {
		return nil, "", fmt.Errorf("decoding nonce: %w", err)
	}

	// Compute shared secret.
	sharedSecret, err := c.keyPair.SharedSecret(senderPublicKey)
	if err != nil {
		return nil, "", fmt.Errorf("computing shared secret: %w", err)
	}

	// Decrypt data.
	data, err := crypto.Decrypt(encryptedData, sharedSecret, nonce)
	if err != nil {
		return nil, "", fmt.Errorf("decrypting data: %w", err)
	}

	return data, downloadResp.Format, nil
}

// --- Inbox support (new endpoints for receiving from peers) ---

type InboxListResponse struct {
	OK      bool               `json:"ok"`
	Pending []InboxPendingItem `json:"pending,omitempty"`
	Error   string             `json:"error,omitempty"`
}

type InboxPendingItem struct {
	SenderDeviceID string `json:"sender_device_id"`
	Fingerprint    string `json:"fingerprint"`
	Format         string `json:"format"`
	Timestamp      int64  `json:"timestamp"`
	BlobID         string `json:"blob_id"`
}

// ListInbox returns the list of pending clips sent to this device by peers.
func (c *Client) ListInbox() (*InboxListResponse, error) {
	req, err := http.NewRequest("GET", c.relayURL+"/api/v1/inbox", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing inbox: %w", err)
	}
	defer resp.Body.Close()

	var listResp InboxListResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &listResp, nil
}

// FetchFromInbox downloads the encrypted blob sent by a specific peer.
func (c *Client) FetchFromInbox(senderDeviceID string) (*DownloadResponse, error) {
	url := c.relayURL + "/api/v1/inbox/" + senderDeviceID
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching from inbox: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return &DownloadResponse{OK: false, Error: "no data from that sender"}, nil
	}

	var downloadResp DownloadResponse
	if err := json.NewDecoder(resp.Body).Decode(&downloadResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &downloadResp, nil
}

// --- Peer upload and listing additions for v1 relay (enables per-peer E2E inbox delivery) ---

// ListPeersResponse is the response for GET /api/v1/peers (my peers).
type ListPeersResponse struct {
	OK    bool         `json:"ok"`
	Peers []DeviceInfo `json:"peers,omitempty"`
	Error string       `json:"error,omitempty"`
}

// ListPeers lists the peers that have been added for this device (with their pubkeys).
func (c *Client) ListPeers() (*ListPeersResponse, error) {
	req, err := http.NewRequest("GET", c.relayURL+"/api/v1/peers", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing peers: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay server error: status %d", resp.StatusCode)
	}

	var listResp ListPeersResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &listResp, nil
}

// UploadTo uploads an already-encrypted blob to a specific receiver's inbox.
func (c *Client) UploadTo(receiverDeviceID, format string, encryptedData, nonce []byte, ttl int) (*UploadResponse, error) {
	payload := UploadPayload{
		DeviceID:  string(c.deviceID),
		Format:    format,
		Data:      base64.StdEncoding.EncodeToString(encryptedData),
		Nonce:     base64.StdEncoding.EncodeToString(nonce),
		Timestamp: time.Now().Unix(),
		TTL:       ttl,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling payload: %w", err)
	}

	url := c.relayURL + "/api/v1/upload/" + receiverDeviceID
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("uploading to relay inbox: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay server error: status %d", resp.StatusCode)
	}

	var uploadResp UploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&uploadResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	if !uploadResp.OK {
		return &uploadResp, fmt.Errorf("upload to peer failed: %s", uploadResp.Error)
	}
	return &uploadResp, nil
}

// EncryptAndUploadTo encrypts data for the given peer pubkey and uploads to that receiver's inbox.
func (c *Client) EncryptAndUploadTo(peerPublicKey *ecdh.PublicKey, receiverDeviceID, format string, data []byte, ttl int) (*UploadResponse, error) {
	sharedSecret, err := c.keyPair.SharedSecret(peerPublicKey)
	if err != nil {
		return nil, fmt.Errorf("computing shared secret: %w", err)
	}
	nonce, err := crypto.GenerateNonce()
	if err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}
	encryptedData, err := crypto.Encrypt(data, sharedSecret, nonce)
	if err != nil {
		return nil, fmt.Errorf("encrypting data: %w", err)
	}
	return c.UploadTo(receiverDeviceID, format, encryptedData, nonce, ttl)
}
