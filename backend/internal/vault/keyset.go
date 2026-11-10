package vault

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/tink-crypto/tink-go/v2/aead"
	"github.com/tink-crypto/tink-go/v2/keyset"
	"github.com/tink-crypto/tink-go/v2/tink"

	"github.com/radch-ai/ava/backend/internal/db"
)

// loadOrMintKeysetAEAD unwraps the vault_keyset blob under master,
// minting and storing it first when the row does not exist (first boot).
// The returned AEAD seals entries; master touches exactly one blob.
func loadOrMintKeysetAEAD(queries *db.Queries, master tink.AEAD) (tink.AEAD, error) {
	wrapped, err := queries.GetVaultKeyset(context.Background())
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return mintKeysetAEAD(queries, master)
	case err != nil:
		return nil, fmt.Errorf("load vault keyset: %w", err)
	}

	blob, err := base64.StdEncoding.DecodeString(wrapped)
	if err != nil {
		return nil, fmt.Errorf("decode vault keyset: %w", err)
	}
	handle, err := keyset.Read(keyset.NewBinaryReader(bytes.NewReader(blob)), master)
	if err != nil {
		return nil, fmt.Errorf(
			"unwrap vault keyset: AVA_SECRETS_MASTER_KEY does not match the key this database was initialized with: %w", err)
	}
	return aead.New(handle)
}

// mintKeysetAEAD generates a fresh AES-256-GCM keyset, persists it wrapped
// under master, and returns its AEAD.
func mintKeysetAEAD(queries *db.Queries, master tink.AEAD) (tink.AEAD, error) {
	handle, err := keyset.NewHandle(aead.AES256GCMKeyTemplate())
	if err != nil {
		return nil, fmt.Errorf("generate vault keyset: %w", err)
	}

	var buf bytes.Buffer
	if err := handle.Write(keyset.NewBinaryWriter(&buf), master); err != nil {
		return nil, fmt.Errorf("wrap vault keyset: %w", err)
	}
	wrapped := base64.StdEncoding.EncodeToString(buf.Bytes())
	if err := queries.InsertVaultKeyset(context.Background(), wrapped); err != nil {
		return nil, fmt.Errorf("store vault keyset: %w", err)
	}
	return aead.New(handle)
}
