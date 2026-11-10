package vault

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/tink-crypto/tink-go/v2/aead"
	"github.com/tink-crypto/tink-go/v2/tink"

	"github.com/radch-ai/ava/backend/internal/db"
)

// ErrNotFound: no entry exists at the exact (vault, name).
var ErrNotFound = errors.New("vault entry not found")

// Vault names: "runtime" holds secrets the app's runtime uses on the
// user's behalf; "secrets" holds user-stored secrets for agent
// consumption.
const (
	NameRuntime = "runtime"
	NameSecrets = "secrets"
)

// EntryMeta is the listable face of an entry — never a secret value.
type EntryMeta struct {
	Name      string
	Kind      string
	Meta      []byte
	UpdatedAt int64
}

// Store reads and writes sealed entries over an already-open,
// already-migrated database.
type Store struct {
	q    *db.Queries
	aead tink.AEAD
}

// NewStore returns a Store over sqlDB, minting the keyset on first boot
// and unwrapping it after. A keyset that does not unwrap under master
// means a different master key initialized this database — fatal at boot.
func NewStore(sqlDB *sql.DB, master tink.AEAD) (*Store, error) {
	queries := db.New(sqlDB)
	handle, err := loadOrMintKeysetHandle(queries, master)
	if err != nil {
		return nil, err
	}
	entryAEAD, err := aead.New(handle)
	if err != nil {
		return nil, fmt.Errorf("vault entry AEAD: %w", err)
	}
	return &Store{q: queries, aead: entryAEAD}, nil
}

// Put seals plaintext and inserts the entry, overwriting any existing
// entry at the same (vault, name).
func (s *Store) Put(ctx context.Context, vaultName, name, kind string, plaintext, meta []byte) error {
	envelope, err := seal(s.aead, vaultName, name, plaintext)
	if err != nil {
		return err
	}
	err = s.q.PutVaultEntry(ctx, db.PutVaultEntryParams{
		Vault:     vaultName,
		Name:      name,
		Kind:      kind,
		Envelope:  envelope,
		Meta:      string(meta),
		UpdatedAt: time.Now().UnixMilli(),
	})
	if err != nil {
		return fmt.Errorf("put vault entry %s/%s: %w", vaultName, name, err)
	}
	return nil
}

// Get returns the plaintext and metadata for the entry. A tampered or
// copied envelope fails here, for this entry only.
func (s *Store) Get(ctx context.Context, vaultName, name string) (plaintext, meta []byte, err error) {
	row, err := s.q.GetVaultEntry(ctx, db.GetVaultEntryParams{Vault: vaultName, Name: name})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("load vault entry %s/%s: %w", vaultName, name, err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(row.Envelope)
	if err != nil {
		return nil, nil, fmt.Errorf("decode vault entry %s/%s: %w", vaultName, name, err)
	}
	plaintext, err = s.aead.Decrypt(ciphertext, associatedData(vaultName, name))
	if err != nil {
		return nil, nil, fmt.Errorf("open vault entry %s/%s: %w", vaultName, name, err)
	}
	return plaintext, []byte(row.Meta), nil
}

// Has reports whether an entry exists at the exact (vault, name) —
// the existence check behind hasKey-style flags; no plaintext.
func (s *Store) Has(ctx context.Context, vaultName, name string) (bool, error) {
	_, err := s.q.GetVaultEntry(ctx, db.GetVaultEntryParams{Vault: vaultName, Name: name})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load vault entry %s/%s: %w", vaultName, name, err)
	}
	return true, nil
}

// Replace seals plaintext over an existing entry; a missing entry is
// ErrNotFound — change means replace, not silently create.
func (s *Store) Replace(ctx context.Context, vaultName, name string, plaintext, meta []byte) error {
	envelope, err := seal(s.aead, vaultName, name, plaintext)
	if err != nil {
		return err
	}
	n, err := s.q.ReplaceVaultEntry(ctx, db.ReplaceVaultEntryParams{
		Envelope:  envelope,
		Meta:      string(meta),
		UpdatedAt: time.Now().UnixMilli(),
		Vault:     vaultName,
		Name:      name,
	})
	if err != nil {
		return fmt.Errorf("replace vault entry %s/%s: %w", vaultName, name, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes the entry; deleting a missing entry returns ErrNotFound.
func (s *Store) Delete(ctx context.Context, vaultName, name string) error {
	n, err := s.q.DeleteVaultEntry(ctx, db.DeleteVaultEntryParams{Vault: vaultName, Name: name})
	if err != nil {
		return fmt.Errorf("delete vault entry %s/%s: %w", vaultName, name, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// List returns entry metadata for one vault, never values.
func (s *Store) List(ctx context.Context, vaultName string) ([]EntryMeta, error) {
	rows, err := s.q.ListVaultEntries(ctx, vaultName)
	if err != nil {
		return nil, fmt.Errorf("list vault entries %s: %w", vaultName, err)
	}
	entries := make([]EntryMeta, 0, len(rows))
	for _, r := range rows {
		entries = append(entries, EntryMeta{
			Name:      r.Name,
			Kind:      r.Kind,
			Meta:      []byte(r.Meta),
			UpdatedAt: r.UpdatedAt,
		})
	}
	return entries, nil
}
