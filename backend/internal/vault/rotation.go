package vault

// Key rotation for the store: master rotation re-wraps one blob; data
// rotation re-seals every row.
import (
	"context"
	"database/sql"
	"fmt"

	"github.com/tink-crypto/tink-go/v2/aead"
	"github.com/tink-crypto/tink-go/v2/keyset"
	"github.com/tink-crypto/tink-go/v2/tink"

	"github.com/radch-ai/ava/backend/internal/db"
)

// RotateMaster re-wraps the stored keyset under newMaster; a master
// that does not unwrap the keyset is the wrong previous key. The
// entry AEAD is unchanged, so no envelope is touched.
func RotateMaster(sqlDB *sql.DB, oldMaster, newMaster tink.AEAD) error {
	ctx := context.Background()
	queries := db.New(sqlDB)

	wrapped, err := queries.GetVaultKeyset(ctx)
	if err != nil {
		return fmt.Errorf("load vault keyset: %w", err)
	}
	handle, err := unwrapKeyset(wrapped, oldMaster)
	if err != nil {
		return err
	}
	rewrapped, err := wrapKeyset(handle, newMaster)
	if err != nil {
		return err
	}
	if err := queries.UpdateVaultKeyset(ctx, rewrapped); err != nil {
		return fmt.Errorf("store rotated keyset: %w", err)
	}
	return nil
}

// RotateKeys adds and promotes a fresh data key, eagerly re-seals
// every entry under it, then deletes the retired key — old database
// copies hold old-key envelopes, and a key absent from the keyset
// opens none of them. One transaction: whole or not at all. Returns
// the number of re-sealed entries.
//
// Deleting the retired key is safe because this tool runs offline in
// the same single process family as the server — no writer can hold
// an old-key envelope past the commit, and the transaction hides the
// both-keys intermediate. A second process writing envelopes while a
// rotation runs would need the retired key kept for a grace period.
func RotateKeys(sqlDB *sql.DB, master tink.AEAD) (int, error) {
	ctx := context.Background()

	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin rotation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	queries := db.New(tx)
	handle, err := loadOrMintKeysetHandle(queries, master)
	if err != nil {
		return 0, err
	}
	oldPrimary, err := handle.Primary()
	if err != nil {
		return 0, fmt.Errorf("read primary key: %w", err)
	}

	manager := keyset.NewManagerFromHandle(handle)
	keyID, err := manager.Add(aead.AES256GCMKeyTemplate())
	if err != nil {
		return 0, fmt.Errorf("mint data key: %w", err)
	}
	if err := manager.SetPrimary(keyID); err != nil {
		return 0, fmt.Errorf("promote data key: %w", err)
	}
	rotated, err := manager.Handle()
	if err != nil {
		return 0, fmt.Errorf("rotated keyset: %w", err)
	}

	entryAEAD, err := aead.New(rotated)
	if err != nil {
		return 0, fmt.Errorf("entry AEAD: %w", err)
	}
	store := Store{q: queries, aead: entryAEAD}

	resealed := 0
	for _, vaultName := range []string{NameRuntime, NameSecrets} {
		entries, err := store.List(ctx, vaultName)
		if err != nil {
			return 0, err
		}
		for _, e := range entries {
			plaintext, meta, err := store.Get(ctx, vaultName, e.Name)
			if err != nil {
				return 0, err
			}
			if err := store.Put(ctx, vaultName, e.Name, e.Kind, plaintext, meta); err != nil {
				return 0, err
			}
			resealed++
		}
	}

	// Every envelope now opens under the new key alone — the retired
	// key leaves the keyset (why that is safe: above).
	if err := manager.Delete(oldPrimary.KeyID()); err != nil {
		return 0, fmt.Errorf("retire old data key: %w", err)
	}
	pruned, err := manager.Handle()
	if err != nil {
		return 0, fmt.Errorf("pruned keyset: %w", err)
	}
	rewrapped, err := wrapKeyset(pruned, master)
	if err != nil {
		return 0, err
	}
	if err := queries.UpdateVaultKeyset(ctx, rewrapped); err != nil {
		return 0, fmt.Errorf("store rotated keyset: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit rotation: %w", err)
	}
	return resealed, nil
}
