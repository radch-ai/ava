package schema_test

// DB-enforced rules for the vault tables; sealing and boot behavior
// live in backend/internal/vault's own tests.
import (
	"testing"

	"github.com/radch-ai/ava/backend/internal/testdb"
)

func TestVaultKeysetIsASingleRow(t *testing.T) {
	db := testdb.Open(t, true)

	if _, err := db.Exec(`INSERT INTO vault_keyset (id, wrapped_keyset) VALUES (1, 'AAAA')`); err != nil {
		t.Fatalf("first keyset row: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO vault_keyset (id, wrapped_keyset) VALUES (1, 'BBBB')`); err == nil {
		t.Fatal("duplicate id = 1 accepted, want primary-key rejection")
	}
	if _, err := db.Exec(`INSERT INTO vault_keyset (id, wrapped_keyset) VALUES (2, 'CCCC')`); err == nil {
		t.Fatal("id = 2 accepted, want CHECK(id = 1) rejection")
	}
}

func TestVaultEntryAddressIsUnique(t *testing.T) {
	db := testdb.Open(t, true)

	insert := `INSERT INTO vault_entries (vault, name, kind, envelope, meta, updated_at)
		VALUES ('runtime', 'provider:p1', 'secret', 'AAAA', '', 0)`
	if _, err := db.Exec(insert); err != nil {
		t.Fatalf("insert entry: %v", err)
	}
	if _, err := db.Exec(insert); err == nil {
		t.Fatal("duplicate (vault, name) accepted, want primary-key rejection")
	}
	// The same name in another vault is a different entry.
	other := `INSERT INTO vault_entries (vault, name, kind, envelope, meta, updated_at)
		VALUES ('secrets', 'provider:p1', 'secret', 'AAAA', '', 0)`
	if _, err := db.Exec(other); err != nil {
		t.Fatalf("same name in another vault: %v", err)
	}
}

func TestVaultEntryVaultIsChecked(t *testing.T) {
	db := testdb.Open(t, true)

	if _, err := db.Exec(`INSERT INTO vault_entries (vault, name, kind, envelope, meta, updated_at)
		VALUES ('runtime', 'provider:p1', 'secret', 'AAAA', '', 0)`); err != nil {
		t.Fatalf("insert runtime entry: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO vault_entries (vault, name, kind, envelope, meta, updated_at)
		VALUES ('secrets', 'github', 'secret', 'AAAA', '', 0)`); err != nil {
		t.Fatalf("insert secrets entry: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO vault_entries (vault, name, kind, envelope, meta, updated_at)
		VALUES ('other', 'x', 'secret', 'AAAA', '', 0)`); err == nil {
		t.Fatal("vault 'other' accepted, want CHECK(vault IN ('runtime', 'secrets')) rejection")
	}
}
