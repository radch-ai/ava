package auth

// Sessions is the session-store contract the middleware sits on: web
// sessions sealed server-side. Deliberately session-shaped — callers
// hand over ids and never see storage CRUD or addresses.

import (
	"context"
	"errors"

	"github.com/radch-ai/ava/backend/internal/vault"
)

type Sessions interface {
	// Create seals a new session under id; meta rides along sealed.
	Create(ctx context.Context, id string, meta []byte) error
	// Verify reports whether id is a live session. A tampered or
	// copied envelope fails here, sealed-entry AEAD being the check.
	Verify(ctx context.Context, id string) error
	// Revoke deletes the session; revoking an unknown id is not an
	// error.
	Revoke(ctx context.Context, id string) error
}

// NewVaultSessions is the vault-backed Sessions: one entry per session
// at runtime/web-session:<id>, the id itself the sealed plaintext —
// the address binding is the integrity check.
func NewVaultSessions(store *vault.Store) Sessions {
	return vaultSessions{store}
}

type vaultSessions struct{ store *vault.Store }

const (
	sessionEntryPrefix = "web-session:"
	sessionKind        = "web-session"
)

func (s vaultSessions) Create(ctx context.Context, id string, meta []byte) error {
	return s.store.Put(ctx, vault.NameRuntime,
		sessionEntryPrefix+id, sessionKind, []byte(id), meta)
}

func (s vaultSessions) Verify(ctx context.Context, id string) error {
	_, _, err := s.store.Get(ctx, vault.NameRuntime, sessionEntryPrefix+id)
	return err
}

func (s vaultSessions) Revoke(ctx context.Context, id string) error {
	err := s.store.Delete(ctx, vault.NameRuntime, sessionEntryPrefix+id)
	if errors.Is(err, vault.ErrNotFound) {
		return nil
	}
	return err
}
