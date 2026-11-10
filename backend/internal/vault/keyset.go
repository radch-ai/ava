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

// loadOrMintKeysetHandle unwraps the vault_keyset blob under master,
// minting and storing it first when the row does not exist (first
// boot). Its keys seal entries; master touches one blob.
func loadOrMintKeysetHandle(queries *db.Queries, master tink.AEAD) (*keyset.Handle, error) {
	wrapped, err := queries.GetVaultKeyset(context.Background())
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return mintKeyset(queries, master)
	case err != nil:
		return nil, fmt.Errorf("load vault keyset: %w", err)
	}
	return unwrapKeyset(wrapped, master)
}

// unwrapKeyset opens one wrapped keyset blob. A blob master cannot
// open means a different master key initialized this database.
func unwrapKeyset(wrapped string, master tink.AEAD) (*keyset.Handle, error) {
	blob, err := base64.StdEncoding.DecodeString(wrapped)
	if err != nil {
		return nil, fmt.Errorf("decode vault keyset: %w", err)
	}
	handle, err := keyset.Read(keyset.NewBinaryReader(bytes.NewReader(blob)), master)
	if err != nil {
		return nil, fmt.Errorf(
			"unwrap vault keyset: AVA_SECRETS_MASTER_KEY does not match the key this database was initialized with: %w", err)
	}
	return handle, nil
}

// wrapKeyset serializes a handle under master.
func wrapKeyset(handle *keyset.Handle, master tink.AEAD) (string, error) {
	var buf bytes.Buffer
	if err := handle.Write(keyset.NewBinaryWriter(&buf), master); err != nil {
		return "", fmt.Errorf("wrap vault keyset: %w", err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// mintKeyset generates a fresh AES-256-GCM keyset, persists it wrapped
// under master, and returns its handle.
func mintKeyset(queries *db.Queries, master tink.AEAD) (*keyset.Handle, error) {
	handle, err := keyset.NewHandle(aead.AES256GCMKeyTemplate())
	if err != nil {
		return nil, fmt.Errorf("generate vault keyset: %w", err)
	}
	wrapped, err := wrapKeyset(handle, master)
	if err != nil {
		return nil, err
	}
	if err := queries.InsertVaultKeyset(context.Background(), wrapped); err != nil {
		return nil, fmt.Errorf("store vault keyset: %w", err)
	}
	return handle, nil
}
