package credential

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

var ErrNotConfigured = errors.New("credential encryption key is not configured")

const (
	CloudflareTokenAAD = "home-gateway/cloudflare-token/v1"
	StorageSecretAAD   = "home-gateway/storage-secret/v1"
)

// Encryptor encrypts secrets with AES-256-GCM.
type Encryptor struct {
	aead cipher.AEAD
}

// FromEnv loads CREDENTIAL_ENCRYPTION_KEY. Missing or invalid values produce an
// unconfigured encryptor whose operations return ErrNotConfigured.
func FromEnv() *Encryptor {
	value := strings.TrimSpace(os.Getenv("CREDENTIAL_ENCRYPTION_KEY"))
	if value == "" {
		return &Encryptor{}
	}
	key, err := decodeKey(value)
	if err != nil {
		return &Encryptor{}
	}
	encryptor, err := New(key)
	if err != nil {
		return &Encryptor{}
	}
	return encryptor
}

// New creates an encryptor from an exact 32-byte key.
func New(key []byte) (*Encryptor, error) {
	if len(key) != 32 {
		return nil, errors.New("credential encryption key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create credential cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create credential GCM: %w", err)
	}
	return &Encryptor{aead: aead}, nil
}

// Encrypt returns ciphertext, nonce, fingerprint, and a safe display hint.
func (e *Encryptor) Encrypt(token string) ([]byte, []byte, string, string, error) {
	return e.EncryptFor(CloudflareTokenAAD, token)
}

// Decrypt recovers a stored API token.
func (e *Encryptor) Decrypt(ciphertext []byte, nonce []byte) (string, error) {
	return e.DecryptFor(CloudflareTokenAAD, ciphertext, nonce)
}

// EncryptFor encrypts a secret under the provided associated data.
func (e *Encryptor) EncryptFor(aad string, secret string) ([]byte, []byte, string, string, error) {
	if e == nil || e.aead == nil {
		return nil, nil, "", "", ErrNotConfigured
	}
	if secret == "" {
		return nil, nil, "", "", errors.New("secret must not be empty")
	}
	if len(secret) > 4096 {
		return nil, nil, "", "", errors.New("secret is too long")
	}
	if strings.TrimSpace(aad) == "" {
		return nil, nil, "", "", errors.New("associated data must not be empty")
	}

	nonce := make([]byte, e.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, "", "", fmt.Errorf("generate credential nonce: %w", err)
	}
	ciphertext := e.aead.Seal(nil, nonce, []byte(secret), []byte(aad))
	sum := sha256.Sum256([]byte(secret))
	hint := secret
	if len(hint) > 4 {
		hint = hint[len(hint)-4:]
	}
	return ciphertext, nonce, hex.EncodeToString(sum[:]), hint, nil
}

// DecryptFor recovers a secret encrypted with EncryptFor.
func (e *Encryptor) DecryptFor(aad string, ciphertext []byte, nonce []byte) (string, error) {
	if e == nil || e.aead == nil {
		return "", ErrNotConfigured
	}
	if len(nonce) != e.aead.NonceSize() {
		return "", errors.New("invalid credential nonce")
	}
	plaintext, err := e.aead.Open(nil, nonce, ciphertext, []byte(aad))
	if err != nil {
		return "", errors.New("decrypt credential: authentication failed")
	}
	return string(plaintext), nil
}

func decodeKey(value string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		key, err = base64.RawStdEncoding.DecodeString(value)
	}
	if err != nil {
		return nil, errors.New("credential encryption key must be Base64")
	}
	if len(key) != 32 {
		return nil, errors.New("credential encryption key must decode to 32 bytes")
	}
	return key, nil
}
