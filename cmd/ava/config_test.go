package main

import (
	"bytes"
	"encoding/hex"
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
