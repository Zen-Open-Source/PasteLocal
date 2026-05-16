package relay

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"

	"github.com/pastelocal/pastelocal/internal/crypto"
)

// Envelope carries per-receiver encrypted payloads.
type Envelope struct {
	V              int              `json:"v"`
	Format         string           `json:"format"`
	SenderDeviceID string           `json:"sender"`
	SenderPubKey   string           `json:"sender_pub"`
	Items          []EnvelopeItem   `json:"items"`
}

type EnvelopeItem struct {
	ReceiverDeviceID string `json:"receiver"`
	Nonce            string `json:"nonce"`
	Ciphertext       string `json:"ct"`
}

// BuildEnvelope builds an envelope for the given receivers (deviceID -> pubkey).
// Data is encrypted per receiver using ECDH(sender_priv, receiver_pub) and AES-GCM.
func BuildEnvelope(format string, data []byte, senderKP *crypto.KeyPair, receivers map[string]*ecdh.PublicKey) ([]byte, error) {
	items := make([]EnvelopeItem, 0, len(receivers))
	for rid, rpub := range receivers {
		secret, err := senderKP.SharedSecret(rpub)
		if err != nil {
			return nil, fmt.Errorf("shared secret: %w", err)
		}
		keyReader := hkdf.New(sha256.New, secret, nil, nil)
		key := make([]byte, 32)
		if _, err := io.ReadFull(keyReader, key); err != nil {
			return nil, fmt.Errorf("derive key: %w", err)
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, fmt.Errorf("aes: %w", err)
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, fmt.Errorf("gcm: %w", err)
		}
		nonce := make([]byte, aead.NonceSize())
		if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
			return nil, fmt.Errorf("nonce: %w", err)
		}
		ct := aead.Seal(nil, nonce, data, nil)
		items = append(items, EnvelopeItem{
			ReceiverDeviceID: rid,
			Nonce:            base64.StdEncoding.EncodeToString(nonce),
			Ciphertext:       base64.StdEncoding.EncodeToString(ct),
		})
	}
	env := Envelope{
		V:              1,
		Format:         format,
		SenderDeviceID: string(senderKP.DeviceID()),
		SenderPubKey:   senderKP.PublicKeyBase64(),
		Items:          items,
	}
	b, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// ParseEnvelope parses an envelope from bytes.
func ParseEnvelope(b []byte) (*Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(b, &env); err != nil {
		return nil, err
	}
	if env.V != 1 {
		return nil, fmt.Errorf("unsupported envelope version")
	}
	return &env, nil
}
