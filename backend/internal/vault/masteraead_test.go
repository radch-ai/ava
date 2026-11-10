package vault_test

// Master-key parsing.
import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/radch-ai/ava/backend/internal/vault"
)

func TestMasterKeyFromHex(t *testing.T) {
	raw := keyBytes(0x42)
	key, err := vault.MasterKeyFromHex(hex.EncodeToString(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if string(key) != string(raw) {
		t.Fatalf("key = %x, want %x", key, raw)
	}
}

func TestMasterKeyFromHexRejects(t *testing.T) {
	cases := map[string]string{
		"not hex":       "zz" + strings.Repeat("0", 62),
		"short":         strings.Repeat("0", 62),
		"empty":         "",
		"trailing junk": strings.Repeat("0", 64) + "\n",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := vault.MasterKeyFromHex(in); err == nil {
				t.Fatalf("parse %q = nil error, want rejection", in)
			}
		})
	}
}
