package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"golang.org/x/crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
)

// KeyPair represents an X25519 keypair.
type KeyPair struct {
	PrivateKey *ecdh.PrivateKey
	PublicKey  *ecdh.PublicKey
}

// DeviceID is the unique identifier for a device (SHA-256 of public key).
type DeviceID string

// GenerateKeyPair generates a new X25519 keypair.
func GenerateKeyPair() (*KeyPair, error) {
	curve := ecdh.X25519()
	privateKey, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating keypair: %w", err)
	}
	publicKey := privateKey.PublicKey()
	return &KeyPair{
		PrivateKey: privateKey,
		PublicKey:  publicKey,
	}, nil
}

// DeviceID returns the device ID (SHA-256 of public key).
func (kp *KeyPair) DeviceID() DeviceID {
	pubKeyBytes := kp.PublicKey.Bytes()
	hash := sha256.Sum256(pubKeyBytes)
	return DeviceID(hex.EncodeToString(hash[:]))
}

// Fingerprint returns a human-readable fingerprint (first 8 bytes of device ID).
func (kp *KeyPair) Fingerprint() string {
	return string(kp.DeviceID()[:16])
}

// PrivateKeyBase64 returns the private key as base64.
func (kp *KeyPair) PrivateKeyBase64() string {
	return base64.StdEncoding.EncodeToString(kp.PrivateKey.Bytes())
}

// PublicKeyBase64 returns the public key as base64.
func (kp *KeyPair) PublicKeyBase64() string {
	return base64.StdEncoding.EncodeToString(kp.PublicKey.Bytes())
}

// LoadKeyPairFromBase64 loads a keypair from base64-encoded private key.
func LoadKeyPairFromBase64(privateKeyB64 string) (*KeyPair, error) {
	curve := ecdh.X25519()
	privateKeyBytes, err := base64.StdEncoding.DecodeString(privateKeyB64)
	if err != nil {
		return nil, fmt.Errorf("decoding private key: %w", err)
	}
	privateKey, err := curve.NewPrivateKey(privateKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("loading private key: %w", err)
	}
	publicKey := privateKey.PublicKey()
	return &KeyPair{
		PrivateKey: privateKey,
		PublicKey:  publicKey,
	}, nil
}

// SharedSecret computes the ECDH shared secret with another device's public key.
func (kp *KeyPair) SharedSecret(peerPublicKey *ecdh.PublicKey) ([]byte, error) {
	return kp.PrivateKey.ECDH(peerPublicKey)
}

// ParsePublicKey parses a base64-encoded public key.
func ParsePublicKey(publicKeyB64 string) (*ecdh.PublicKey, error) {
	curve := ecdh.X25519()
	publicKeyBytes, err := base64.StdEncoding.DecodeString(publicKeyB64)
	if err != nil {
		return nil, fmt.Errorf("decoding public key: %w", err)
	}
	return curve.NewPublicKey(publicKeyBytes)
}

// Encrypt encrypts data using AES-GCM with a key derived via HKDF-SHA256.
// The derived key is 32 bytes and the nonce must match GCM's nonce size (12 bytes).
func Encrypt(plaintext []byte, sharedSecret []byte, nonce []byte) ([]byte, error) {
	keyReader := hkdf.New(sha256.New, sharedSecret, nil, nil)
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
	if len(nonce) != aead.NonceSize() {
		return nil, fmt.Errorf("invalid nonce size: got %d want %d", len(nonce), aead.NonceSize())
	}
	ct := aead.Seal(nil, nonce, plaintext, nil)
	return ct, nil
}

// Decrypt decrypts data using AES-GCM with a key derived via HKDF-SHA256.
func Decrypt(ciphertext []byte, sharedSecret []byte, nonce []byte) ([]byte, error) {
	keyReader := hkdf.New(sha256.New, sharedSecret, nil, nil)
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
	if len(nonce) != aead.NonceSize() {
		return nil, fmt.Errorf("invalid nonce size: got %d want %d", len(nonce), aead.NonceSize())
	}
	pt, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return pt, nil
}

// GenerateNonce generates a random nonce for encryption.
func GenerateNonce() ([]byte, error) {
	// AES-GCM uses a 12-byte nonce.
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}
	return nonce, nil
}
