package vault_test

// Store behavior over the real migration chain — round-trips, keyset
// boot custody, fail-closed associated data, replace/delete. Table
// rules live in backend/internal/schema. Seeding and tampering go
// through the query layer; raw SQL only where no query exists.
import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
	"github.com/radch-ai/ava/backend/internal/vault"
	"github.com/radch-ai/ava/backend/sqlstore"
)

func keyBytes(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }

func newStore(t *testing.T, db *sql.DB, key []byte) *vault.Store {
	t.Helper()
	master, err := vault.NewMasterAEAD(key)
	if err != nil {
		t.Fatalf("master aead: %v", err)
	}
	store, err := vault.NewStore(db, master)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store
}

func openMemoryDB(t *testing.T) *sql.DB {
	t.Helper()
	return testdb.Open(t)
}

// seedFileDB creates a file-backed database whose keyset was minted under
// keyBytes(0xA1) with one sealed entry, closed and ready to reopen.
func seedFileDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vault.db")
	db, err := sqlstore.OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	store := newStore(t, db, keyBytes(0xA1))
	if err := store.Put(context.Background(), "runtime", "provider:p1", "secret", []byte("sk-live-1"), []byte(`{"k":"v"}`)); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}
	return path
}

func TestPutGetRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := newStore(t, openMemoryDB(t), keyBytes(0xA1))

	plaintext, meta := []byte("sk-live-1234"), []byte(`{"source":"settings"}`)
	if err := store.Put(ctx, "runtime", "provider:p1", "secret", plaintext, meta); err != nil {
		t.Fatalf("put: %v", err)
	}

	gotPlaintext, gotMeta, err := store.Get(ctx, "runtime", "provider:p1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !bytes.Equal(gotPlaintext, plaintext) {
		t.Fatalf("plaintext = %q, want %q", gotPlaintext, plaintext)
	}
	if !bytes.Equal(gotMeta, meta) {
		t.Fatalf("meta = %q, want %q", gotMeta, meta)
	}
}

func TestReopenWithSameMasterKey(t *testing.T) {
	path := seedFileDB(t)

	db, err := sqlstore.OpenSQLite(path)
	if err != nil {
		t.Fatalf("reopen sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()

	store := newStore(t, db, keyBytes(0xA1)) // mint once, open many
	got, _, err := store.Get(context.Background(), "runtime", "provider:p1")
	if err != nil {
		t.Fatalf("get after reopen: %v", err)
	}
	if string(got) != "sk-live-1" {
		t.Fatalf("plaintext after reopen = %q, want %q", got, "sk-live-1")
	}
}

func TestWrongMasterKeyFailsBoot(t *testing.T) {
	path := seedFileDB(t)

	db, err := sqlstore.OpenSQLite(path)
	if err != nil {
		t.Fatalf("reopen sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()

	master, err := vault.NewMasterAEAD(keyBytes(0xB2))
	if err != nil {
		t.Fatalf("master aead: %v", err)
	}
	_, err = vault.NewStore(db, master)
	if err == nil {
		t.Fatal("boot with wrong master key accepted, want failure")
	}
	if !strings.Contains(err.Error(), "does not match the key this database was initialized with") {
		t.Fatalf("error = %v, want the mismatch message", err)
	}
}

func TestKeysetTamperRejected(t *testing.T) {
	path := seedFileDB(t)

	sqlDB, err := sqlstore.OpenSQLite(path)
	if err != nil {
		t.Fatalf("reopen sqlite: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	// Tamper through the query layer's keyset write.
	if err := db.New(sqlDB).UpdateVaultKeyset(context.Background(), "AAAA"); err != nil {
		t.Fatalf("tamper keyset: %v", err)
	}

	master, err := vault.NewMasterAEAD(keyBytes(0xA1))
	if err != nil {
		t.Fatalf("master aead: %v", err)
	}
	if _, err := vault.NewStore(sqlDB, master); err == nil {
		t.Fatal("tampered keyset accepted, want failure")
	}
}

func TestCopiedEnvelopeFailsClosed(t *testing.T) {
	ctx := context.Background()
	sqlDB := openMemoryDB(t)
	store := newStore(t, sqlDB, keyBytes(0xA1))
	if err := store.Put(ctx, "runtime", "provider:p1", "secret", []byte("sk-live-1"), nil); err != nil {
		t.Fatalf("put: %v", err)
	}
	queries := db.New(sqlDB)

	// Same envelope re-addressed to a different vault or name: each
	// must fail at the AEAD tag, while the original still opens.
	copies := [][2]string{
		{"secrets", "provider:p1"},
		{"runtime", "provider:p2"},
	}
	src, err := queries.GetVaultEntry(ctx, db.GetVaultEntryParams{
		Vault: "runtime",
		Name:  "provider:p1",
	})
	if err != nil {
		t.Fatalf("read source envelope: %v", err)
	}
	for _, c := range copies {
		if err := queries.PutVaultEntry(ctx, db.PutVaultEntryParams{
			Vault:     c[0],
			Name:      c[1],
			Kind:      "secret",
			Envelope:  src.Envelope,
			Meta:      src.Meta,
			UpdatedAt: time.Now().UnixMilli(),
		}); err != nil {
			t.Fatalf("copy to %v: %v", c, err)
		}
		if _, _, err := store.Get(ctx, c[0], c[1]); err == nil {
			t.Fatalf("re-addressed envelope %v opened, want fail-closed", c)
		}
	}

	if got, _, err := store.Get(ctx, "runtime", "provider:p1"); err != nil || string(got) != "sk-live-1" {
		t.Fatalf("original after copies = (%q, %v), want it intact", got, err)
	}
}

func TestReplaceSemantics(t *testing.T) {
	ctx := context.Background()
	store := newStore(t, openMemoryDB(t), keyBytes(0xA1))

	// Put upserts: second Put overwrites the first.
	if err := store.Put(ctx, "secrets", "github", "secret", []byte("old"), []byte(`{"v":1}`)); err != nil {
		t.Fatalf("put old: %v", err)
	}
	if err := store.Put(ctx, "secrets", "github", "secret", []byte("new"), []byte(`{"v":2}`)); err != nil {
		t.Fatalf("put new: %v", err)
	}
	if got, meta, err := store.Get(ctx, "secrets", "github"); err != nil || string(got) != "new" || string(meta) != `{"v":2}` {
		t.Fatalf("after re-put = (%q, %q, %v), want overwritten values", got, meta, err)
	}

	// Replace refuses to create.
	if err := store.Replace(ctx, "secrets", "missing", []byte("x"), nil); !errors.Is(err, vault.ErrNotFound) {
		t.Fatalf("replace missing = %v, want ErrNotFound", err)
	}

	// Replace overwrites an existing entry.
	if err := store.Replace(ctx, "secrets", "github", []byte("replaced"), []byte(`{"v":3}`)); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if got, meta, err := store.Get(ctx, "secrets", "github"); err != nil || string(got) != "replaced" || string(meta) != `{"v":3}` {
		t.Fatalf("after replace = (%q, %q, %v), want replaced values", got, meta, err)
	}
}

func TestDeleteAndList(t *testing.T) {
	ctx := context.Background()
	db := openMemoryDB(t)
	store := newStore(t, db, keyBytes(0xA1))
	if err := store.Put(ctx, "secrets", "github", "secret", []byte("pw"), []byte(`{"u":"a"}`)); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := store.Put(ctx, "secrets", "registry", "secret", []byte("pw2"), nil); err != nil {
		t.Fatalf("put: %v", err)
	}

	entries, err := store.List(ctx, "secrets")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 2 || entries[0].Name != "github" || entries[1].Name != "registry" {
		t.Fatalf("list = %+v, want github+registry ordered by name", entries)
	}
	if entries[0].Kind != "secret" || string(entries[0].Meta) != `{"u":"a"}` || entries[0].UpdatedAt == 0 {
		t.Fatalf("entry meta = %+v, want kind/meta/updated_at populated", entries[0])
	}

	// List is scoped to one vault.
	runtimeEntries, err := store.List(ctx, "runtime")
	if err != nil {
		t.Fatalf("list runtime: %v", err)
	}
	if len(runtimeEntries) != 0 {
		t.Fatalf("runtime list = %+v, want empty", runtimeEntries)
	}

	if err := store.Delete(ctx, "secrets", "github"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, _, err := store.Get(ctx, "secrets", "github"); !errors.Is(err, vault.ErrNotFound) {
		t.Fatalf("get after delete = %v, want ErrNotFound", err)
	}
	if err := store.Delete(ctx, "secrets", "github"); !errors.Is(err, vault.ErrNotFound) {
		t.Fatalf("delete again = %v, want ErrNotFound", err)
	}
}
