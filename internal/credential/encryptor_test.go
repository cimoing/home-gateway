package credential

import (
	"bytes"
	"errors"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	encryptor, err := New(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}

	ciphertext, nonce, fingerprint, hint, err := encryptor.Encrypt("cloudflare-secret-token")
	if err != nil {
		t.Fatal(err)
	}
	if string(ciphertext) == "cloudflare-secret-token" {
		t.Fatal("token was not encrypted")
	}
	if fingerprint == "" || hint != "oken" {
		t.Fatalf("unexpected metadata: fingerprint=%q hint=%q", fingerprint, hint)
	}

	token, err := encryptor.Decrypt(ciphertext, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if token != "cloudflare-secret-token" {
		t.Fatalf("unexpected decrypted token %q", token)
	}

	ciphertext[0] ^= 0xff
	if _, err := encryptor.Decrypt(ciphertext, nonce); err == nil {
		t.Fatal("expected tampered ciphertext to fail")
	}
}

func TestUnconfiguredEncryptor(t *testing.T) {
	encryptor := &Encryptor{}
	if _, _, _, _, err := encryptor.Encrypt("token"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}
}
