package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// MasterAEAD is the stdlib AES-256-GCM adapter implementing Tink's AEAD
// interface over the master key. It wraps only the keyset blob in the
// database, never secret rows; key sourcing is the caller's concern.
type MasterAEAD struct {
	gcm cipher.AEAD
}

// NewMasterAEAD builds the adapter over a raw 32-byte key.
func NewMasterAEAD(key []byte) (*MasterAEAD, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("master key: got %d bytes, want 32", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("master key cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("master key GCM: %w", err)
	}
	return &MasterAEAD{gcm: gcm}, nil
}

// Encrypt seals plaintext under a fresh random nonce, prefixing the nonce
// to the ciphertext.
func (m *MasterAEAD) Encrypt(plaintext, associatedData []byte) ([]byte, error) {
	nonce := make([]byte, m.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("master AEAD nonce: %w", err)
	}
	return m.gcm.Seal(nonce, nonce, plaintext, associatedData), nil
}

// Decrypt opens a ciphertext produced by Encrypt.
func (m *MasterAEAD) Decrypt(ciphertext, associatedData []byte) ([]byte, error) {
	if len(ciphertext) < m.gcm.NonceSize() {
		return nil, errors.New("master AEAD ciphertext too short")
	}
	nonce, sealed := ciphertext[:m.gcm.NonceSize()], ciphertext[m.gcm.NonceSize():]
	plaintext, err := m.gcm.Open(nil, nonce, sealed, associatedData)
	if err != nil {
		return nil, fmt.Errorf("master AEAD decrypt: %w", err)
	}
	return plaintext, nil
}
