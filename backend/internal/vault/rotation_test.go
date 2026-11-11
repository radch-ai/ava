package vault_test

// Rotation over the real store: master rotation re-wraps only the
// keyset blob; data-key rotation re-seals every entry whole.
import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/tink-crypto/tink-go/v2/keyset"

	"github.com/radch-ai/ava/backend/internal/testdb"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// envelopes snapshots every stored envelope, keyed by vault/name.
func envelopes(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	rows, err := db.Query(`SELECT vault, name, envelope FROM vault_entries`)
	if err != nil {
		t.Fatalf("query envelopes: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var vaultName, name, envelope string
		if err := rows.Scan(&vaultName, &name, &envelope); err != nil {
			t.Fatalf("scan envelope: %v", err)
		}
		out[vaultName+"/"+name] = envelope
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("envelope rows: %v", err)
	}
	return out
}

// Master rotation touches exactly one thing: the wrapped keyset. No
// envelope changes; the new key boots, the old one no longer does.
func TestRotateMasterRewrapsKeysetOnly(t *testing.T) {
	db := testdb.Open(t, true)
	store := newStore(t, db, keyBytes(0xA1))
	ctx := context.Background()
	for _, e := range []struct{ vaultName, name string }{
		{"runtime", "provider:p1"}, {"secrets", "openai-key"},
	} {
		if err := store.Put(ctx, e.vaultName, e.name, "secret", []byte("v-"+e.name), nil); err != nil {
			t.Fatalf("put %s: %v", e.name, err)
		}
	}
	before := envelopes(t, db)

	oldMaster, err := vault.NewMasterAEAD(keyBytes(0xA1))
	if err != nil {
		t.Fatalf("old master: %v", err)
	}
	newMaster, err := vault.NewMasterAEAD(keyBytes(0xB2))
	if err != nil {
		t.Fatalf("new master: %v", err)
	}
	if err := vault.RotateMaster(db, oldMaster, newMaster); err != nil {
		t.Fatalf("rotate master: %v", err)
	}

	if after := envelopes(t, db); len(after) != len(before) {
		t.Fatalf("entry count changed: %d, want %d", len(after), len(before))
	} else {
		for k, v := range before {
			if after[k] != v {
				t.Fatalf("master rotation re-sealed %s", k)
			}
		}
	}

	reopened := newStore(t, db, keyBytes(0xB2))
	for _, e := range []struct{ vaultName, name string }{
		{"runtime", "provider:p1"}, {"secrets", "openai-key"},
	} {
		plaintext, _, err := reopened.Get(ctx, e.vaultName, e.name)
		if err != nil {
			t.Fatalf("get after rotation: %v", err)
		}
		if string(plaintext) != "v-"+e.name {
			t.Fatalf("value = %q, want %q", plaintext, "v-"+e.name)
		}
	}

	if _, err := vault.NewMasterAEAD(keyBytes(0xA1)); err != nil {
		t.Fatalf("old master aead: %v", err)
	}
	master, err := vault.NewMasterAEAD(keyBytes(0xA1))
	if err != nil {
		t.Fatalf("old master aead: %v", err)
	}
	if _, err := vault.NewStore(db, master); err == nil {
		t.Fatal("old master key still boots, want unwrap failure")
	}
}

func TestRotateMasterWrongOldKey(t *testing.T) {
	db := testdb.Open(t, true)
	newStore(t, db, keyBytes(0xA1))

	wrongMaster, err := vault.NewMasterAEAD(keyBytes(0xC3))
	if err != nil {
		t.Fatalf("wrong master: %v", err)
	}
	newMaster, err := vault.NewMasterAEAD(keyBytes(0xB2))
	if err != nil {
		t.Fatalf("new master: %v", err)
	}
	err = vault.RotateMaster(db, wrongMaster, newMaster)
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("err = %v, want the wrong-master-key failure", err)
	}
}

// Data-key rotation re-seals every entry under the new primary — every
// envelope changes, every plaintext survives.
func TestRotateKeysResealsEveryEntry(t *testing.T) {
	db := testdb.Open(t, true)
	store := newStore(t, db, keyBytes(0xA1))
	ctx := context.Background()
	for _, e := range []struct{ vaultName, name, value string }{
		{"runtime", "provider:p1", "k-1"},
		{"runtime", "provider:p2", "k-2"},
		{"secrets", "openai-key", "sk-1"},
	} {
		if err := store.Put(ctx, e.vaultName, e.name, "secret", []byte(e.value), nil); err != nil {
			t.Fatalf("put %s: %v", e.name, err)
		}
	}
	before := envelopes(t, db)

	master, err := vault.NewMasterAEAD(keyBytes(0xA1))
	if err != nil {
		t.Fatalf("master: %v", err)
	}
	if _, err := vault.RotateKeys(db, master); err != nil {
		t.Fatalf("rotate keys: %v", err)
	}

	after := envelopes(t, db)
	if len(after) != len(before) {
		t.Fatalf("entry count = %d, want %d", len(after), len(before))
	}
	for k, v := range before {
		if after[k] == v {
			t.Fatalf("entry %s kept its envelope, want re-seal", k)
		}
	}

	reopened := newStore(t, db, keyBytes(0xA1))
	for _, e := range []struct{ vaultName, name, value string }{
		{"runtime", "provider:p1", "k-1"},
		{"runtime", "provider:p2", "k-2"},
		{"secrets", "openai-key", "sk-1"},
	} {
		plaintext, _, err := reopened.Get(ctx, e.vaultName, e.name)
		if err != nil {
			t.Fatalf("get %s after rotation: %v", e.name, err)
		}
		if string(plaintext) != e.value {
			t.Fatalf("value = %q, want %q", plaintext, e.value)
		}
	}

	// The retired key is gone from the keyset — old DB copies hold
	// old-key envelopes nothing can open anymore.
	var wrapped string
	if err := db.QueryRow(`SELECT wrapped_keyset FROM vault_keyset WHERE id = 1`).Scan(&wrapped); err != nil {
		t.Fatalf("read keyset row: %v", err)
	}
	blob, err := base64.StdEncoding.DecodeString(wrapped)
	if err != nil {
		t.Fatalf("decode keyset: %v", err)
	}
	handle, err := keyset.Read(keyset.NewBinaryReader(bytes.NewReader(blob)), master)
	if err != nil {
		t.Fatalf("unwrap keyset: %v", err)
	}
	if handle.Len() != 1 {
		t.Fatalf("keyset holds %d keys, want 1 (retired key deleted)", handle.Len())
	}
}

func TestRotateKeysOnEmptyVault(t *testing.T) {
	db := testdb.Open(t, true)
	master, err := vault.NewMasterAEAD(keyBytes(0xA1))
	if err != nil {
		t.Fatalf("master: %v", err)
	}
	if _, err := vault.RotateKeys(db, master); err != nil {
		t.Fatalf("rotate keys on empty vault: %v", err)
	}
	newStore(t, db, keyBytes(0xA1))
}
