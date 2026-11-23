package testdb

import (
	"database/sql"
	"testing"

	"github.com/radch-ai/ava/backend/internal/db"
)

// SeedMcpServer inserts one server row with the defaults every caller
// seeds by hand otherwise; opts mutate the params before the insert
// (URL, timeout, approvals).
func SeedMcpServer(t *testing.T, sqlDB *sql.DB, id, url string, opts ...func(*db.CreateMcpServerParams)) {
	t.Helper()
	arg := db.CreateMcpServerParams{
		ID:                  id,
		Name:                id,
		Url:                 sql.NullString{String: url, Valid: url != ""},
		HeaderKeys:          "[]",
		AllowPrivateNetwork: 1,
		AllowPlaintext:      1,
	}
	for _, opt := range opts {
		opt(&arg)
	}
	if err := db.New(sqlDB).CreateMcpServer(t.Context(), arg); err != nil {
		t.Fatalf("seed mcp server %q: %v", id, err)
	}
}
