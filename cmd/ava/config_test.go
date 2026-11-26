package main

import (
	"bytes"
	"encoding/hex"
	"os"
	"testing"

	"github.com/rs/zerolog"
)

func TestLoadConfig(t *testing.T) {
	master := bytes.Repeat([]byte{0x42}, 32)
	t.Setenv("AVA_ADDR", "127.0.0.1:9999")
	t.Setenv("AVA_DB", "/tmp/some-ava.db")
	t.Setenv("AVA_SECRETS_MASTER_KEY", hex.EncodeToString(master))
	t.Setenv("AVA_UI_TOKEN", "ui-token-xyz")
	cfg := loadConfig(zerolog.Nop())

	if cfg.Addr != "127.0.0.1:9999" {
		t.Errorf("addr = %q, want 127.0.0.1:9999", cfg.Addr)
	}
	if cfg.DBPath != "/tmp/some-ava.db" {
		t.Errorf("db path = %q, want /tmp/some-ava.db", cfg.DBPath)
	}
	if !bytes.Equal(cfg.MasterKey, master) {
		t.Errorf("master key = %x, want %x", cfg.MasterKey, master)
	}
	if cfg.Token != "ui-token-xyz" {
		t.Errorf("ui token = %q, want ui-token-xyz", cfg.Token)
	}
}

// The catalog source has three states: unset takes the upstream
// default, a value overrides it, and an explicit empty value disables
// refresh — the kill switch GetEnv could not express.
func TestModelCatalogURL(t *testing.T) {
	master := bytes.Repeat([]byte{0x42}, 32)
	t.Setenv("AVA_DB", "/tmp/some-ava.db")
	t.Setenv("AVA_SECRETS_MASTER_KEY", hex.EncodeToString(master))
	t.Setenv("AVA_UI_TOKEN", "ui-token-xyz")

	cases := []struct {
		name  string
		set   bool
		value string
		want  string
	}{
		{name: "unset takes the upstream default", set: false, want: defaultModelCatalogURL},
		{name: "override", set: true, value: "http://catalog.internal/api.json", want: "http://catalog.internal/api.json"},
		{name: "explicit empty disables refresh", set: true, value: "", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// t.Setenv restores after the subtest; unsetting needs its
			// own restore so the cases cannot leak into each other.
			if prior, ok := os.LookupEnv("AVA_MODEL_CATALOG_URL"); ok {
				t.Cleanup(func() { _ = os.Setenv("AVA_MODEL_CATALOG_URL", prior) })
			}
			if tc.set {
				t.Setenv("AVA_MODEL_CATALOG_URL", tc.value)
			} else if err := os.Unsetenv("AVA_MODEL_CATALOG_URL"); err != nil {
				t.Fatalf("unset: %v", err)
			}
			if got := loadConfig(zerolog.Nop()).ModelCatalogURL; got != tc.want {
				t.Errorf("ModelCatalogURL = %q, want %q", got, tc.want)
			}
		})
	}
}
