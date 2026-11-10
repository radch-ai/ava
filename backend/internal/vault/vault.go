// Package vault is secret storage: entries are sealed to their
// (vault, name) address, so a row moved elsewhere fails closed at the
// AEAD tag. vault is "runtime" (app-managed, e.g. provider api keys)
// or "secrets" (user-stored, for agents). Row keys live in one Tink
// keyset, stored in vault_keyset wrapped by a master key that never
// touches rows; sourcing it is the caller's concern.
package vault

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"

	"github.com/tink-crypto/tink-go/v2/tink"
)

// associatedData binds a ciphertext to its address — length-prefixed so
// different pairs cannot collide by concatenation.
func associatedData(vaultName, name string) []byte {
	var ad []byte
	for _, part := range [...]string{vaultName, name} {
		var lenPrefix [4]byte
		binary.BigEndian.PutUint32(lenPrefix[:], uint32(len(part)))
		ad = append(ad, lenPrefix[:]...)
		ad = append(ad, part...)
	}
	return ad
}

// seal encrypts plaintext bound to its (vault, name) address.
func seal(aead tink.AEAD, vaultName, name string, plaintext []byte) (string, error) {
	ciphertext, err := aead.Encrypt(plaintext, associatedData(vaultName, name))
	if err != nil {
		return "", fmt.Errorf("seal vault entry %s/%s: %w", vaultName, name, err)
	}
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}
