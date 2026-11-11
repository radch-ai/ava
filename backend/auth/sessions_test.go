package auth_test

// The vault-backed Sessions store; direct store peeks prove where and
// how entries land.

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/radch-ai/ava/backend/auth"
	"github.com/radch-ai/ava/backend/internal/testdb"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// newSessions returns NewVaultSessions over a real store, plus the store
// itself for peeks.
func newSessions(t *testing.T) (auth.Sessions, *vault.Store) {
	t.Helper()

	master, err := vault.NewMasterAEAD(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("vault.NewMasterAEAD: %v", err)
	}
	store, err := vault.NewStore(testdb.Open(t, true), master)
	if err != nil {
		t.Fatalf("vault.NewStore: %v", err)
	}
	return auth.NewVaultSessions(store), store
}

func TestSessionCreateSealsAtOwnAddress(t *testing.T) {
	sessions, peek := newSessions(t)
	meta := []byte(`{"created":"2026-11-10T00:00:00Z"}`)

	if err := sessions.Create(context.Background(), "sess-id-1", meta); err != nil {
		t.Fatalf("Create: %v", err)
	}

	plaintext, sealedMeta, err := peek.Get(context.Background(),
		vault.NameRuntime, "web-session:sess-id-1")
	if err != nil {
		t.Fatalf("vault Get session: %v", err)
	}
	if string(plaintext) != "sess-id-1" {
		t.Errorf("sealed plaintext = %q, want the session id", plaintext)
	}
	if string(sealedMeta) != string(meta) {
		t.Errorf("sealed meta = %s, want the caller's meta verbatim", sealedMeta)
	}
}

func TestSessionVerifyRoundTrip(t *testing.T) {
	sessions, _ := newSessions(t)
	ctx := context.Background()

	if err := sessions.Create(ctx, "sess-id-2", nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := sessions.Verify(ctx, "sess-id-2"); err != nil {
		t.Errorf("Verify live session: %v, want nil", err)
	}
	if err := sessions.Verify(ctx, "never-minted"); err == nil {
		t.Error("Verify never-minted session = nil, want error")
	}
}

func TestSessionRevokeDeletesEntry(t *testing.T) {
	sessions, peek := newSessions(t)
	ctx := context.Background()

	if err := sessions.Create(ctx, "sess-id-3", nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := sessions.Revoke(ctx, "sess-id-3"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := sessions.Verify(ctx, "sess-id-3"); err == nil {
		t.Error("Verify revoked session = nil, want error")
	}
	if _, _, err := peek.Get(ctx, vault.NameRuntime, "web-session:sess-id-3"); !errors.Is(err, vault.ErrNotFound) {
		t.Errorf("vault Get revoked session = %v, want ErrNotFound", err)
	}
}

func TestSessionRevokeMissingIsNotError(t *testing.T) {
	sessions, _ := newSessions(t)

	if err := sessions.Revoke(context.Background(), "never-minted"); err != nil {
		t.Errorf("Revoke missing session = %v, want nil", err)
	}
}
