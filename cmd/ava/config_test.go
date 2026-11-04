package main

import (
	"testing"

	"github.com/rs/zerolog"
)

func TestLoadConfig(t *testing.T) {
	t.Setenv("AVA_ADDR", "127.0.0.1:9999")
	t.Setenv("AVA_DB", "/tmp/some-ava.db")

	cfg := loadConfig(zerolog.Nop())

	if cfg.Addr != "127.0.0.1:9999" {
		t.Errorf("addr = %q, want 127.0.0.1:9999", cfg.Addr)
	}
	if cfg.DBPath != "/tmp/some-ava.db" {
		t.Errorf("db path = %q, want /tmp/some-ava.db", cfg.DBPath)
	}
}
